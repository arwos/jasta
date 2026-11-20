package owner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"golang.org/x/crypto/argon2"
)

const (
	passwordSaltBytes = 16
	passwordHashBytes = 32
	argonTime         = 3
	argonMemory       = 64 * 1024
	argonThreads      = 2
	recoveryCodeCount = 10
	recoveryCodeBytes = 16
)

var (
	ErrAlreadyBootstrapped = errors.New("owner is already bootstrapped")
	ErrInvalidEnrollment   = errors.New("TOTP enrollment code is invalid")
	ErrInvalidPassword     = errors.New("password must contain 12 to 1024 bytes")
)

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Enrollment contains the one-time TOTP secret and provisioning URL. Callers
// must show them only to the local operator and discard them after setup.
type Enrollment struct {
	Secret string
	URL    string
}

// BeginEnrollment creates a new TOTP secret for a local owner setup.
func BeginEnrollment(issuer, account string) (Enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: issuer, AccountName: account, Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return Enrollment{}, fmt.Errorf("generate TOTP enrollment: %w", err)
	}
	return Enrollment{Secret: key.Secret(), URL: key.URL()}, nil
}

// Bootstrap creates the sole owner only after the enrollment code is valid.
// The TOTP secret is encrypted at rest and recovery codes are returned once;
// only their hashes are stored.
func Bootstrap(ctx context.Context, database orm.Stmt, password, secret, passcode string, encryptionKey []byte) ([]string, error) {
	if database == nil {
		return nil, errors.New("database statement is required")
	}
	if len(password) < 12 || len(password) > 1024 {
		return nil, ErrInvalidPassword
	}
	if len(encryptionKey) != 32 {
		return nil, errors.New("TOTP encryption key must contain exactly 32 bytes")
	}
	valid, err := totp.ValidateCustom(passcode, secret, time.Now(), totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !valid {
		return nil, ErrInvalidEnrollment
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	codec, err := aesgcm.New(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("create TOTP encryption codec: %w", err)
	}
	sealedSecret, err := codec.Encrypt([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}

	err = database.CallContext(ctx, "bootstrap local owner", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		var exists int
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM owners WHERE id = 1").Scan(&exists)
		if err == nil {
			return ErrAlreadyBootstrapped
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check existing owner: %w", err)
		}
		now := time.Now().Unix()
		if _, err = tx.ExecContext(ctx, `INSERT INTO owners (id, password_hash, totp_secret_ciphertext, totp_confirmed, created_at, updated_at)
			VALUES (1, ?, ?, 1, ?, ?)`, passwordHash, sealedSecret, now, now); err != nil {
			return fmt.Errorf("insert owner: %w", err)
		}
		for _, hash := range hashes {
			if _, err = tx.ExecContext(ctx, "INSERT INTO owner_recovery_codes (code_hash, created_at, used_at) VALUES (?, ?, NULL)", hash, now); err != nil {
				return fmt.Errorf("insert recovery code hash: %w", err)
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// OwnerExists reports whether the database has the single owner row.
func OwnerExists(ctx context.Context, database orm.Stmt) (bool, error) {
	if database == nil {
		return false, errors.New("database statement is required")
	}
	exists := false
	err := database.CallContext(ctx, "check local owner", func(ctx context.Context, db orm.DB) error {
		var value int
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		err := queryer.QueryRowContext(ctx, "SELECT 1 FROM owners WHERE id = 1").Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		exists = true
		return nil
	})
	return exists, err
}

// Reset removes the owner, recovery codes, and all refresh sessions atomically.
func Reset(ctx context.Context, database orm.Stmt) error {
	if database == nil {
		return errors.New("database statement is required")
	}
	return database.CallContext(ctx, "reset local owner", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err = tx.ExecContext(ctx, "DELETE FROM owner_recovery_codes"); err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE auth_sessions SET replaced_by_id = NULL"); err != nil {
			return fmt.Errorf("unlink refresh sessions: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM auth_sessions"); err != nil {
			return fmt.Errorf("delete refresh sessions: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM owners WHERE id = 1"); err != nil {
			return fmt.Errorf("delete owner: %w", err)
		}
		return tx.Commit()
	})
}

// ConsumeRecoveryCode atomically consumes a valid unused recovery code.
func ConsumeRecoveryCode(ctx context.Context, database orm.Stmt, code string) (bool, error) {
	if database == nil {
		return false, errors.New("database statement is required")
	}
	hash := recoveryCodeHash(code)
	consumed := false
	err := database.CallContext(ctx, "consume owner recovery code", func(ctx context.Context, db orm.DB) error {
		result, err := db.ExecContext(ctx, "UPDATE owner_recovery_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL", time.Now().Unix(), hash)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		consumed = count == 1
		return nil
	})
	return consumed, err
}

// HashPassword returns a self-describing Argon2id password hash.
func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", ErrInvalidPassword
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, passwordHashBytes)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword checks a stored HashPassword value and refuses unsafe parameters.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid password hash format")
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", argonMemory, argonTime, argonThreads) {
		return false, errors.New("invalid password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != passwordSaltBytes {
		return false, errors.New("invalid password hash salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != passwordHashBytes {
		return false, errors.New("invalid password hash digest")
	}
	actual := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func newRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		value := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(value); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		encoded := strings.TrimRight(base32.StdEncoding.EncodeToString(value), "=")
		code := encoded[:8] + "-" + encoded[8:16] + "-" + encoded[16:]
		codes = append(codes, code)
		hashes = append(hashes, recoveryCodeHash(code))
	}
	return codes, hashes, nil
}

func recoveryCodeHash(code string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	digest := sha256.Sum256([]byte("jasta-owner-recovery-v1:" + normalized))
	return hex.EncodeToString(digest[:])
}
