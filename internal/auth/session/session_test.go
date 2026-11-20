package session

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/storage"
)

func TestLoginJWTStableAcrossManagerRestartAndRefreshRotationLogout(t *testing.T) {
	ctx := context.Background()
	database := sessionDatabase(t)
	stmt := database.Tag("master")
	key, secret, _ := bootstrapOwner(t, stmt)
	manager, err := New(key)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	code := currentTOTP(t, secret)
	first, err := manager.Login(ctx, stmt, "a-long-owner-password", code)
	if err != nil {
		t.Fatalf("Login(): %v", err)
	}
	claims, err := manager.VerifyAccess(first.AccessToken)
	if err != nil || claims.Subject != "owner" || claims.SessionID == "" {
		t.Fatalf("VerifyAccess() claims=%#v err=%v", claims, err)
	}
	header, _, err := manager.signer.Verify([]byte(first.AccessToken))
	if err != nil {
		t.Fatalf("verify JWT header: %v", err)
	}
	if lifetime := header.ExpiresAt - header.IssuedAt; lifetime != int64(AccessLifetime/time.Second) {
		t.Fatalf("JWT declared lifetime=%d seconds, want %d", lifetime, int64(AccessLifetime/time.Second))
	}
	if first.AccessExpiresAt.Sub(time.Now()) > AccessLifetime || first.AccessExpiresAt.Sub(time.Now()) < AccessLifetime-time.Second {
		t.Fatalf("access lifetime=%s, want %s", time.Until(first.AccessExpiresAt), AccessLifetime)
	}
	if first.RefreshExpiresAt.Sub(time.Now()) > SessionLifetime || first.RefreshExpiresAt.Sub(time.Now()) < SessionLifetime-time.Second {
		t.Fatalf("refresh lifetime=%s, want %s", time.Until(first.RefreshExpiresAt), SessionLifetime)
	}
	restarted, err := New(key)
	if err != nil {
		t.Fatalf("New() after restart: %v", err)
	}
	if _, err = restarted.VerifyAccess(first.AccessToken); err != nil {
		t.Fatalf("restarted manager rejected access token: %v", err)
	}
	wrongKey := []byte("abcdef0123456789abcdef0123456789")
	otherManager, err := New(wrongKey)
	if err != nil {
		t.Fatalf("New() with a different key: %v", err)
	}
	if _, err = otherManager.VerifyAccess(first.AccessToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("different-key manager error=%v, want ErrInvalidCredentials", err)
	}
	mutated := "A" + first.AccessToken[1:]
	if _, err = manager.VerifyAccess(mutated); err == nil {
		t.Fatal("VerifyAccess() accepted a modified JWT")
	}
	if _, err = manager.Login(ctx, stmt, "wrong-long-owner-password", currentTOTP(t, secret)); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() wrong password error=%v, want ErrInvalidCredentials", err)
	}
	if _, err = manager.Login(ctx, stmt, "a-long-owner-password", "bad-code"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() wrong second factor error=%v, want ErrInvalidCredentials", err)
	}
	if count := sessionCount(t, stmt); count != 1 {
		t.Fatalf("session count after failed logins=%d, want 1", count)
	}
	if err = stmt.CallContext(ctx, "verify refresh token is stored as a hash", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		var storedHash []byte
		var expiry int64
		if err := queryer.QueryRowContext(ctx, "SELECT refresh_token_hash, expires_at FROM auth_sessions WHERE id = ?", claims.SessionID).Scan(&storedHash, &expiry); err != nil {
			return err
		}
		if string(storedHash) == first.RefreshToken || string(storedHash) != string(refreshTokenHash(first.RefreshToken)) {
			return errors.New("refresh token was not stored as the expected hash")
		}
		if expiry != first.RefreshExpiresAt.Unix() {
			return errors.New("stored refresh expiry differs from the returned expiry")
		}
		return nil
	}); err != nil {
		t.Fatalf("verify stored refresh token hash: %v", err)
	}

	second, err := restarted.Refresh(ctx, stmt, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh(): %v", err)
	}
	if !second.RefreshExpiresAt.Equal(first.RefreshExpiresAt) {
		t.Fatalf("rotation extended absolute refresh expiry: first=%s second=%s", first.RefreshExpiresAt, second.RefreshExpiresAt)
	}
	if _, err = manager.Refresh(ctx, stmt, first.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused refresh token error=%v, want ErrInvalidCredentials", err)
	}
	if err = manager.Logout(ctx, stmt, first.RefreshToken); err != nil {
		t.Fatalf("Logout(): %v", err)
	}
	if _, err = manager.Refresh(ctx, stmt, second.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh() after logout error=%v, want ErrInvalidCredentials", err)
	}
}

func TestRecoveryCodeCreatesOnlyOneLoginAndRevokeSessionStopsRefresh(t *testing.T) {
	ctx := context.Background()
	database := sessionDatabase(t)
	stmt := database.Tag("master")
	key, _, recoveryCodes := bootstrapOwner(t, stmt)
	manager, err := New(key)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	pair, err := manager.Login(ctx, stmt, "a-long-owner-password", recoveryCodes[0])
	if err != nil {
		t.Fatalf("Login() with recovery code: %v", err)
	}
	if _, err = manager.Login(ctx, stmt, "a-long-owner-password", recoveryCodes[0]); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused recovery code error=%v, want ErrInvalidCredentials", err)
	}
	if count := sessionCount(t, stmt); count != 1 {
		t.Fatalf("session count after recovery replay=%d, want 1", count)
	}
	rotated, err := manager.Refresh(ctx, stmt, pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() after recovery login: %v", err)
	}
	claims, err := manager.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess(): %v", err)
	}
	if err = manager.RevokeSession(ctx, stmt, claims.SessionID); err != nil {
		t.Fatalf("RevokeSession(): %v", err)
	}
	if _, err = manager.Refresh(ctx, stmt, rotated.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh() revoked session error=%v, want ErrInvalidCredentials", err)
	}
}

func TestRefreshRejectsExpiredSessionAndAccessTokenExpires(t *testing.T) {
	ctx := context.Background()
	database := sessionDatabase(t)
	stmt := database.Tag("master")
	key, secret, _ := bootstrapOwner(t, stmt)
	manager, err := New(key)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	pair, err := manager.Login(ctx, stmt, "a-long-owner-password", currentTOTP(t, secret))
	if err != nil {
		t.Fatalf("Login(): %v", err)
	}
	if err = stmt.CallContext(ctx, "expire refresh session", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE auth_sessions SET expires_at = ? WHERE refresh_token_hash = ?", time.Now().Unix()-1, refreshTokenHash(pair.RefreshToken))
		return err
	}); err != nil {
		t.Fatalf("expire refresh session: %v", err)
	}
	if _, err = manager.Refresh(ctx, stmt, pair.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh() expired token error=%v, want ErrInvalidCredentials", err)
	}
	manager.now = func() time.Time { return time.Now().Add(AccessLifetime + time.Second) }
	if _, err = manager.VerifyAccess(pair.AccessToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("VerifyAccess() expired token error=%v, want ErrInvalidCredentials", err)
	}
}

func TestRevokeSessionStopsRefresh(t *testing.T) {
	ctx := context.Background()
	database := sessionDatabase(t)
	stmt := database.Tag("master")
	key, secret, _ := bootstrapOwner(t, stmt)
	manager, err := New(key)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	pair, err := manager.Login(ctx, stmt, "a-long-owner-password", currentTOTP(t, secret))
	if err != nil {
		t.Fatalf("Login(): %v", err)
	}
	claims, err := manager.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess(): %v", err)
	}
	if err = manager.RevokeSession(ctx, stmt, claims.SessionID); err != nil {
		t.Fatalf("RevokeSession(): %v", err)
	}
	if _, err = manager.Refresh(ctx, stmt, pair.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh() revoked token error=%v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRejectsUnconfirmedOwner(t *testing.T) {
	ctx := context.Background()
	database := sessionDatabase(t)
	stmt := database.Tag("master")
	key, secret, _ := bootstrapOwner(t, stmt)
	manager, err := New(key)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if err = stmt.CallContext(ctx, "unconfirm owner TOTP", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE owners SET totp_confirmed = 0 WHERE id = 1")
		return err
	}); err != nil {
		t.Fatalf("unconfirm owner TOTP: %v", err)
	}
	if _, err = manager.Login(ctx, stmt, "a-long-owner-password", currentTOTP(t, secret)); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() unconfirmed owner error=%v, want ErrInvalidCredentials", err)
	}
	if count := sessionCount(t, stmt); count != 0 {
		t.Fatalf("session count for unconfirmed owner=%d, want 0", count)
	}
}

func sessionDatabase(t *testing.T) orm.ORM {
	t.Helper()
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "sessions.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err = storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	return database
}

func bootstrapOwner(t *testing.T, stmt orm.Stmt) ([]byte, string, []string) {
	t.Helper()
	key := []byte("0123456789abcdef0123456789abcdef")
	enrollment, err := owner.BeginEnrollment("Jasta test", "owner")
	if err != nil {
		t.Fatalf("BeginEnrollment(): %v", err)
	}
	codes, err := owner.Bootstrap(context.Background(), stmt, "a-long-owner-password", enrollment.Secret, currentTOTP(t, enrollment.Secret), key)
	if err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}
	return key, enrollment.Secret, codes
}

func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode(): %v", err)
	}
	return code
}

func sessionCount(t *testing.T, stmt orm.Stmt) int {
	t.Helper()
	var count int
	err := stmt.CallContext(context.Background(), "count auth sessions", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		return queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_sessions").Scan(&count)
	})
	if err != nil {
		t.Fatalf("sessionCount(): %v", err)
	}
	return count
}
