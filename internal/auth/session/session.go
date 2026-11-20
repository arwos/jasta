package session

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/auth/token"
	"go.osspkg.com/goppy/v3/plugins/auth/token/algorithm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"golang.org/x/crypto/hkdf"

	"go.arwos.org/jasta/internal/auth/owner"
)

const (
	AccessLifetime  = 15 * time.Minute
	SessionLifetime = 7 * 24 * time.Hour
	issuer          = "jasta-admin"
	audience        = "jasta-admin"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Manager struct {
	signer     token.Token
	signingKey ed25519.PrivateKey
	keyID      string
	key        []byte
	now        func() time.Time
}

type Tokens struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type AccessClaims struct {
	Subject   string `json:"sub"`
	SessionID string `json:"sid"`
}

func (c AccessClaims) MarshalJSON() ([]byte, error) {
	type claims AccessClaims
	return json.Marshal(claims(c))
}

// New derives a stable, purpose-specific Ed25519 JWT key from the root-owned
// master key. Reconstructing the manager with the same key preserves tokens.
func New(masterKey []byte) (*Manager, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("authentication master key must contain exactly 32 bytes")
	}
	reader := hkdf.New(sha256.New, masterKey, []byte("go.arwos.org/jasta/v1"), []byte("admin-jwt-ed25519-signing-key"))
	seed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(reader, seed); err != nil {
		return nil, fmt.Errorf("derive JWT signing key: %w", err)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	clear(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyIDHash := sha256.Sum256(publicKey)
	keyID := hex.EncodeToString(keyIDHash[:8])
	config := token.Config{
		Option: token.ConfigOption{HeaderName: "Authorization", Audience: audience},
		Sign: token.ConfigSign{
			Issuer: issuer,
			Type:   token.TypeJWT,
			Keys: []token.ConfigKey{{
				ID: keyID, Algo: algorithm.EdDSA,
				Key:  token.KeyLink("raw,base64:" + base64.StdEncoding.EncodeToString(privateKey)),
				Cert: token.KeyLink("raw,base64:" + base64.StdEncoding.EncodeToString(publicKey)),
			}},
		},
	}
	signer, err := token.New(config)
	if err != nil {
		clear(privateKey)
		return nil, fmt.Errorf("configure stable JWT signer: %w", err)
	}
	keyCopy := append([]byte(nil), masterKey...)
	privateKeyCopy := append(ed25519.PrivateKey(nil), privateKey...)
	clear(privateKey)
	return &Manager{signer: signer, signingKey: privateKeyCopy, keyID: keyID, key: keyCopy, now: time.Now}, nil
}

// Login verifies the owner password plus either TOTP or one unused recovery
// code, then creates a refresh session and a short-lived access JWT.
func (m *Manager) Login(ctx context.Context, database orm.Stmt, password, secondFactor string) (Tokens, error) {
	if m == nil || m.signer == nil || database == nil || len(password) == 0 || len(secondFactor) == 0 {
		return Tokens{}, ErrInvalidCredentials
	}
	credentials, found, err := loadOwnerCredentials(ctx, database)
	if err != nil {
		return Tokens{}, fmt.Errorf("load owner credentials: %w", err)
	}
	if !found {
		return Tokens{}, ErrInvalidCredentials
	}
	passwordValid, err := owner.VerifyPassword(credentials.passwordHash, password)
	if err != nil || !passwordValid || credentials.confirmed != 1 || len(credentials.sealedTOTPSecret) == 0 {
		return Tokens{}, ErrInvalidCredentials
	}
	codec, err := aesgcm.New(m.key)
	if err != nil {
		return Tokens{}, fmt.Errorf("configure TOTP decryption: %w", err)
	}
	secret, err := codec.Decrypt(credentials.sealedTOTPSecret)
	if err != nil {
		return Tokens{}, fmt.Errorf("decrypt owner TOTP secret: %w", err)
	}
	useRecoveryCode := !validTOTP(secondFactor, string(secret), m.now())
	now := m.now()
	refreshToken, err := newRefreshToken()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	sessionID, err := uuid.NewRandom()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh session ID: %w", err)
	}
	tokens, err := m.issueTokens(sessionID.String(), refreshToken, now.Add(SessionLifetime))
	if err != nil {
		return Tokens{}, err
	}
	err = m.createLoginSession(ctx, database, credentials.passwordHash, secondFactor, useRecoveryCode, sessionID.String(), refreshToken, tokens, now)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return Tokens{}, ErrInvalidCredentials
		}
		return Tokens{}, err
	}
	return tokens, nil
}

type ownerCredentials struct {
	passwordHash     string
	sealedTOTPSecret []byte
	confirmed        int
}

func loadOwnerCredentials(ctx context.Context, database orm.Stmt) (ownerCredentials, bool, error) {
	var credentials ownerCredentials
	found := false
	err := database.CallContext(ctx, "load owner for login", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		err := queryer.QueryRowContext(ctx, "SELECT password_hash, totp_secret_ciphertext, totp_confirmed FROM owners WHERE id = 1").Scan(&credentials.passwordHash, &credentials.sealedTOTPSecret, &credentials.confirmed)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	return credentials, found, err
}

func validTOTP(code, secret string, now time.Time) bool {
	valid, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && valid
}

func (m *Manager) createLoginSession(ctx context.Context, database orm.Stmt, passwordHash, secondFactor string, useRecoveryCode bool, sessionID, refreshToken string, tokens Tokens, now time.Time) error {
	return database.CallContext(ctx, "create owner auth session", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var currentHash string
		var currentConfirmed int
		err = tx.QueryRowContext(ctx, "SELECT password_hash, totp_confirmed FROM owners WHERE id = 1").Scan(&currentHash, &currentConfirmed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return fmt.Errorf("recheck owner credentials: %w", err)
		}
		if currentHash != passwordHash || currentConfirmed != 1 {
			return ErrInvalidCredentials
		}
		if useRecoveryCode {
			consumed, consumeErr := consumeRecoveryCodeTx(ctx, tx, secondFactor, now.Unix())
			if consumeErr != nil {
				return consumeErr
			}
			if !consumed {
				return ErrInvalidCredentials
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO auth_sessions
			(id, refresh_token_hash, created_at, last_used_at, expires_at, revoked_at, replaced_by_id)
			VALUES (?, ?, ?, ?, ?, NULL, NULL)`, sessionID, refreshTokenHash(refreshToken), now.Unix(), now.Unix(), tokens.RefreshExpiresAt.Unix()); err != nil {
			return fmt.Errorf("insert refresh session: %w", err)
		}
		return tx.Commit()
	})
}

// Refresh consumes a refresh token and atomically replaces it with a new one.
// Rotations retain the original seven-day expiration rather than extending it.
func (m *Manager) Refresh(ctx context.Context, database orm.Stmt, refreshToken string) (Tokens, error) {
	if m == nil || m.signer == nil || database == nil || !validRefreshTokenShape(refreshToken) {
		return Tokens{}, ErrInvalidCredentials
	}
	now := m.now()
	newRefresh, err := newRefreshToken()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	newSessionID, err := uuid.NewRandom()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh session ID: %w", err)
	}
	var tokens Tokens
	err = database.CallContext(ctx, "rotate owner refresh session", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		var oldSessionID string
		var expiresAt int64
		var revokedAt sql.NullInt64
		var replacedBy sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT id, expires_at, revoked_at, replaced_by_id
			FROM auth_sessions WHERE refresh_token_hash = ?`, refreshTokenHash(refreshToken)).Scan(&oldSessionID, &expiresAt, &revokedAt, &replacedBy)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return fmt.Errorf("load refresh session: %w", err)
		}
		if revokedAt.Valid || replacedBy.Valid || expiresAt <= now.Unix() {
			return ErrInvalidCredentials
		}
		var confirmed int
		err = tx.QueryRowContext(ctx, "SELECT totp_confirmed FROM owners WHERE id = 1").Scan(&confirmed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return fmt.Errorf("check configured owner: %w", err)
		}
		if confirmed != 1 {
			return ErrInvalidCredentials
		}
		tokens, err = m.issueTokens(newSessionID.String(), newRefresh, time.Unix(expiresAt, 0))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO auth_sessions
			(id, refresh_token_hash, created_at, last_used_at, expires_at, revoked_at, replaced_by_id)
			VALUES (?, ?, ?, ?, ?, NULL, NULL)`, newSessionID.String(), refreshTokenHash(newRefresh), now.Unix(), now.Unix(), expiresAt); err != nil {
			return fmt.Errorf("insert rotated refresh session: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET last_used_at = ?, revoked_at = ?, replaced_by_id = ?
			WHERE id = ? AND revoked_at IS NULL AND replaced_by_id IS NULL AND expires_at > ?`,
			now.Unix(), now.Unix(), newSessionID.String(), oldSessionID, now.Unix())
		if err != nil {
			return fmt.Errorf("revoke rotated refresh session: %w", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrInvalidCredentials
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return Tokens{}, ErrInvalidCredentials
		}
		return Tokens{}, err
	}
	return tokens, nil
}

// Logout revokes the matching refresh session and all rotated descendants.
// Unknown tokens are treated as already logged out so callers cannot use
// logout to probe session state.
func (m *Manager) Logout(ctx context.Context, database orm.Stmt, refreshToken string) error {
	if m == nil || database == nil || !validRefreshTokenShape(refreshToken) {
		return nil
	}
	now := m.now().Unix()
	return database.CallContext(ctx, "logout owner refresh session", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `WITH RECURSIVE session_chain(id) AS (
			SELECT id FROM auth_sessions WHERE refresh_token_hash = ?
			UNION
			SELECT child.id FROM auth_sessions child
			JOIN session_chain parent ON child.id = (SELECT replaced_by_id FROM auth_sessions WHERE id = parent.id)
		)
		UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, ?), last_used_at = ?
		WHERE id IN (SELECT id FROM session_chain)`, refreshTokenHash(refreshToken), now, now)
		return err
	})
}

// RevokeSession revokes a session and its rotated descendants by the ID carried
// in a verified access JWT.
func (m *Manager) RevokeSession(ctx context.Context, database orm.Stmt, sessionID string) error {
	if m == nil || database == nil {
		return errors.New("session manager and database are required")
	}
	if _, err := uuid.Parse(sessionID); err != nil {
		return ErrInvalidCredentials
	}
	now := m.now().Unix()
	return database.CallContext(ctx, "revoke owner auth session", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `WITH RECURSIVE session_chain(id) AS (
			SELECT id FROM auth_sessions WHERE id = ?
			UNION
			SELECT child.id FROM auth_sessions child
			JOIN session_chain parent ON child.id = (SELECT replaced_by_id FROM auth_sessions WHERE id = parent.id)
		)
		UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, ?), last_used_at = ?
		WHERE id IN (SELECT id FROM session_chain)`, sessionID, now, now)
		return err
	})
}

// SessionActive reports whether the verified access token's backing refresh
// session is still current. This makes logout, refresh rotation, and local
// owner reset take effect for access tokens immediately.
func (m *Manager) SessionActive(ctx context.Context, database orm.Stmt, sessionID string) (bool, error) {
	if m == nil || database == nil {
		return false, errors.New("session manager and database are required")
	}
	if _, err := uuid.Parse(sessionID); err != nil {
		return false, ErrInvalidCredentials
	}
	active := false
	err := database.CallContext(ctx, "check owner auth session", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		var revokedAt sql.NullInt64
		var replacedBy sql.NullString
		var expiresAt int64
		err := queryer.QueryRowContext(ctx, `SELECT revoked_at, replaced_by_id, expires_at
			FROM auth_sessions WHERE id = ?`, sessionID).Scan(&revokedAt, &replacedBy, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		active = !revokedAt.Valid && !replacedBy.Valid && expiresAt > m.now().Unix()
		return nil
	})
	return active, err
}

// VerifyAccess verifies signature, key, issuer, audience, lifetime, and claims.
func (m *Manager) VerifyAccess(raw string) (AccessClaims, error) {
	if m == nil || m.signer == nil || raw == "" || len(raw) > 8192 {
		return AccessClaims{}, ErrInvalidCredentials
	}
	header, payload, err := m.signer.Verify([]byte(raw))
	if err != nil || header == nil || header.Algorithm != algorithm.EdDSA || header.TokenType != token.TypeJWT || header.Issuer != issuer || header.Audience != audience || header.TokenID == "" {
		return AccessClaims{}, ErrInvalidCredentials
	}
	now := m.now()
	issuedAt := time.Unix(header.IssuedAt, 0)
	expiresAt := time.Unix(header.ExpiresAt, 0)
	if issuedAt.After(now) || !expiresAt.After(now) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > AccessLifetime {
		return AccessClaims{}, ErrInvalidCredentials
	}
	if _, err = uuid.Parse(header.TokenID); err != nil {
		return AccessClaims{}, ErrInvalidCredentials
	}
	var claims AccessClaims
	if err = json.Unmarshal(payload, &claims); err != nil || claims.Subject != "owner" {
		return AccessClaims{}, ErrInvalidCredentials
	}
	if _, err = uuid.Parse(claims.SessionID); err != nil {
		return AccessClaims{}, ErrInvalidCredentials
	}
	return claims, nil
}

func (m *Manager) issueTokens(sessionID, refreshToken string, refreshExpiry time.Time) (Tokens, error) {
	now := m.now()
	claims := AccessClaims{Subject: "owner", SessionID: sessionID}
	access, err := m.createAccessJWT(claims, now)
	if err != nil {
		return Tokens{}, fmt.Errorf("create owner access JWT: %w", err)
	}
	header, _, err := m.signer.Verify(access)
	if err != nil {
		return Tokens{}, fmt.Errorf("verify newly created access JWT: %w", err)
	}
	return Tokens{
		AccessToken:      string(access),
		AccessExpiresAt:  time.Unix(header.ExpiresAt, 0),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: refreshExpiry.Truncate(time.Second),
	}, nil
}

func (m *Manager) createAccessJWT(claims AccessClaims, now time.Time) ([]byte, error) {
	tokenID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate access token ID: %w", err)
	}
	header := token.Header{
		Algorithm: algorithm.EdDSA,
		TokenType: token.TypeJWT,
		TokenID:   tokenID.String(),
		Issuer:    issuer,
		KeyID:     m.keyID,
		Audience:  audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(AccessLifetime).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("marshal access JWT header: %w", err)
	}
	payloadJSON, err := claims.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal access JWT claims: %w", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(m.signingKey, []byte(signingInput))
	return []byte(signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)), nil
}

func consumeRecoveryCodeTx(ctx context.Context, tx *sql.Tx, code string, now int64) (bool, error) {
	hash := owner.HashRecoveryCode(code)
	result, err := tx.ExecContext(ctx, "UPDATE owner_recovery_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL", now, hash)
	if err != nil {
		return false, fmt.Errorf("consume owner recovery code: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func newRefreshToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validRefreshTokenShape(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func refreshTokenHash(value string) []byte {
	digest := sha256.Sum256([]byte("jasta-refresh-v1:" + value))
	return digest[:]
}
