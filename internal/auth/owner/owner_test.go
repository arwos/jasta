package owner

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/storage"
)

func TestBootstrapConfirmsTOTPAndStoresOnlyRecoveryHashes(t *testing.T) {
	ctx := context.Background()
	database := ownerDatabase(t)
	stmt := database.Tag("master")
	enrollment, err := BeginEnrollment("Jasta Test", "owner")
	if err != nil {
		t.Fatalf("BeginEnrollment(): %v", err)
	}
	password := "long-enough-test-password"
	if _, err = Bootstrap(ctx, stmt, password, enrollment.Secret, "not-a-code", testEncryptionKey()); !errors.Is(err, ErrInvalidEnrollment) {
		t.Fatalf("Bootstrap() with wrong code error = %v, want ErrInvalidEnrollment", err)
	}
	exists, err := OwnerExists(ctx, stmt)
	if err != nil || exists {
		t.Fatalf("owner after invalid code: exists=%v err=%v", exists, err)
	}
	passcode, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode(): %v", err)
	}
	codes, err := Bootstrap(ctx, stmt, password, enrollment.Secret, passcode, testEncryptionKey())
	if err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("recovery code count=%d, want %d", len(codes), recoveryCodeCount)
	}
	exists, err = OwnerExists(ctx, stmt)
	if err != nil || !exists {
		t.Fatalf("owner after bootstrap: exists=%v err=%v", exists, err)
	}
	var passwordHash string
	var encryptedSecret []byte
	var confirmed int
	if err = stmt.CallContext(ctx, "inspect bootstrapped owner", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		return queryer.QueryRowContext(ctx, "SELECT password_hash, totp_secret_ciphertext, totp_confirmed FROM owners WHERE id = 1").Scan(&passwordHash, &encryptedSecret, &confirmed)
	}); err != nil {
		t.Fatalf("inspect owner: %v", err)
	}
	if confirmed != 1 || len(encryptedSecret) == 0 || strings.Contains(string(encryptedSecret), enrollment.Secret) {
		t.Fatalf("confirmed=%d encrypted secret invalid: %x", confirmed, encryptedSecret)
	}
	codec, err := aesgcm.New(testEncryptionKey())
	if err != nil {
		t.Fatalf("create verification codec: %v", err)
	}
	decryptedSecret, err := codec.Decrypt(encryptedSecret)
	if err != nil || string(decryptedSecret) != enrollment.Secret {
		t.Fatalf("decrypt stored TOTP secret: value matches=%v err=%v", string(decryptedSecret) == enrollment.Secret, err)
	}
	valid, err := VerifyPassword(passwordHash, password)
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() valid=%v err=%v", valid, err)
	}
	valid, err = VerifyPassword(passwordHash, "another-long-password")
	if err != nil || valid {
		t.Fatalf("VerifyPassword() for wrong password valid=%v err=%v", valid, err)
	}
	for _, code := range codes {
		if err = stmt.CallContext(ctx, "verify recovery hash", func(ctx context.Context, db orm.DB) error {
			queryer := db.(interface {
				QueryRowContext(context.Context, string, ...any) *sql.Row
			})
			var stored string
			if err := queryer.QueryRowContext(ctx, "SELECT code_hash FROM owner_recovery_codes WHERE code_hash = ?", recoveryCodeHash(code)).Scan(&stored); err != nil {
				return err
			}
			if stored == code {
				return errors.New("plain recovery code was stored")
			}
			return nil
		}); err != nil {
			t.Fatalf("verify recovery code hash: %v", err)
		}
	}
	consumed, err := ConsumeRecoveryCode(ctx, stmt, codes[0])
	if err != nil || !consumed {
		t.Fatalf("first recovery consume: consumed=%v err=%v", consumed, err)
	}
	consumed, err = ConsumeRecoveryCode(ctx, stmt, codes[0])
	if err != nil || consumed {
		t.Fatalf("second recovery consume: consumed=%v err=%v", consumed, err)
	}
	_, err = Bootstrap(ctx, stmt, "different-valid-password", enrollment.Secret, passcode, testEncryptionKey())
	if !errors.Is(err, ErrAlreadyBootstrapped) {
		t.Fatalf("second Bootstrap() error = %v, want ErrAlreadyBootstrapped", err)
	}
	valid, err = VerifyPassword(passwordHash, password)
	if err != nil || !valid {
		t.Fatalf("owner password after rejected second bootstrap valid=%v err=%v", valid, err)
	}
}

func TestResetRemovesOwnerRecoveryCodesAndRefreshSessions(t *testing.T) {
	ctx := context.Background()
	database := ownerDatabase(t)
	stmt := database.Tag("master")
	seedOwner(t, stmt)
	if err := stmt.CallContext(ctx, "seed refresh sessions", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_sessions (id, refresh_token_hash, created_at, last_used_at, expires_at, revoked_at, replaced_by_id)
			VALUES ('old', X'01', 1, 1, 100, NULL, NULL), ('new', X'02', 2, 2, 100, NULL, NULL)`)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "UPDATE auth_sessions SET replaced_by_id = 'new' WHERE id = 'old'")
		return err
	}); err != nil {
		t.Fatalf("seed refresh sessions: %v", err)
	}
	if err := Reset(ctx, stmt); err != nil {
		t.Fatalf("Reset(): %v", err)
	}
	exists, err := OwnerExists(ctx, stmt)
	if err != nil || exists {
		t.Fatalf("owner after reset: exists=%v err=%v", exists, err)
	}
	if err = stmt.CallContext(ctx, "verify reset state", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		var recoveryCount, sessionCount int
		if err := queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM owner_recovery_codes").Scan(&recoveryCount); err != nil {
			return err
		}
		if err := queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_sessions").Scan(&sessionCount); err != nil {
			return err
		}
		if recoveryCount != 0 || sessionCount != 0 {
			return errors.New("reset left owner recovery codes or refresh sessions")
		}
		return nil
	}); err != nil {
		t.Fatalf("verify reset state: %v", err)
	}
}

func TestVerifyPasswordRejectsMalformedStoredHash(t *testing.T) {
	if _, err := VerifyPassword("$argon2id$v=19$m=1,t=1,p=1$bad$bad", "password"); err == nil {
		t.Fatal("VerifyPassword() accepted unsafe hash parameters")
	}
}

func ownerDatabase(t *testing.T) orm.ORM {
	t.Helper()
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "owner.sqlite"), Cache: "private", Mode: "rwc",
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

func seedOwner(t *testing.T, stmt orm.Stmt) {
	t.Helper()
	if err := stmt.CallContext(context.Background(), "seed owner", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "INSERT INTO owners (id, password_hash, totp_confirmed, created_at, updated_at) VALUES (1, 'hash', 1, 1, 1)")
		return err
	}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := stmt.CallContext(context.Background(), "seed recovery code", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "INSERT INTO owner_recovery_codes (code_hash, created_at) VALUES ('hash', 1)")
		return err
	}); err != nil {
		t.Fatalf("seed recovery code: %v", err)
	}
}

func testEncryptionKey() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}
