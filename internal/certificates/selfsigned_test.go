package certificates

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/storage"
)

func TestCreateSelfSignedStoresLocalSANCertificateOutsideReleaseRoot(t *testing.T) {
	ctx := context.Background()
	database := newCertificateTestDB(t, ctx)
	root := filepath.Join(t.TempDir(), "certificates")
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	result, err := CreateSelfSigned(ctx, database.Tag("master"), root, "ADMIN.Localhost.", now)
	if err != nil {
		t.Fatalf("CreateSelfSigned(): %v", err)
	}
	if result.Domain != "admin.localhost" || result.Issuer != "self_signed" || result.Status != "valid" || result.NotAfter.Sub(result.NotBefore) != SelfSignedLifetime+5*time.Minute {
		t.Fatalf("certificate metadata=%#v", result)
	}
	if strings.Contains(result.CertificatePath, "releases") || strings.Contains(result.PrivateKeyPath, "releases") {
		t.Fatalf("certificate was stored in a release directory: %#v", result)
	}
	certificatePEM, err := os.ReadFile(result.CertificatePath)
	if err != nil {
		t.Fatalf("read certificate PEM: %v", err)
	}
	block, rest := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatalf("invalid certificate PEM block: %#v", block)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(): %v", err)
	}
	if err = certificate.VerifyHostname("admin.localhost"); err != nil {
		t.Fatalf("VerifyHostname(): %v", err)
	}
	if certificate.IsCA {
		t.Fatal("self-signed server certificate is marked as a CA")
	}
	if err = certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature); err != nil {
		t.Fatalf("self-signed certificate signature is invalid: %v", err)
	}
	keyInfo, err := os.Stat(result.PrivateKeyPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o640 {
		t.Fatalf("private key mode=%v, want 0640", keyInfo.Mode().Perm())
	}
	directoryInfo, err := os.Stat(filepath.Dir(result.PrivateKeyPath))
	if err != nil {
		t.Fatalf("stat certificate directory: %v", err)
	}
	if directoryInfo.Mode().Perm() != 0o750 {
		t.Fatalf("certificate directory mode=%v, want 0750", directoryInfo.Mode().Perm())
	}
	keyPEM, err := os.ReadFile(result.PrivateKeyPath)
	if err != nil {
		t.Fatalf("read private key PEM: %v", err)
	}
	keyBlock, rest := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(rest) != 0 {
		t.Fatalf("invalid private key PEM: %#v", keyBlock)
	}
	if _, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
		t.Fatalf("ParsePKCS8PrivateKey(): %v", err)
	}
	var count int
	if err = database.Tag("master").CallContext(ctx, "verify certificate metadata", func(ctx context.Context, db orm.DB) error {
		rows, queryErr := db.QueryContext(ctx, "SELECT COUNT(*) FROM certificates WHERE domain = 'admin.localhost' AND issuer = 'self_signed'")
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&count)
	}); err != nil {
		t.Fatalf("read certificate metadata: %v", err)
	}
	if count != 1 {
		t.Fatalf("certificate metadata rows=%d, want 1", count)
	}
}

func TestCreateSelfSignedRejectsPublicOrWildcardDomains(t *testing.T) {
	ctx := context.Background()
	database := newCertificateTestDB(t, ctx)
	root := filepath.Join(t.TempDir(), "certificates")
	for _, domain := range []string{"example.com", "*.localhost", "192.0.2.1", "admin.localhost:8443"} {
		if _, err := CreateSelfSigned(ctx, database.Tag("master"), root, domain, time.Now()); err == nil {
			t.Errorf("CreateSelfSigned(%q) succeeded, want local-domain validation error", domain)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid domains created certificate storage root: stat err=%v", err)
	}
}

func TestCreateSelfSignedRejectsDuplicateDomainWithoutOrphanFiles(t *testing.T) {
	ctx := context.Background()
	database := newCertificateTestDB(t, ctx)
	root := filepath.Join(t.TempDir(), "certificates")
	if _, err := CreateSelfSigned(ctx, database.Tag("master"), root, "admin.localhost", time.Now()); err != nil {
		t.Fatalf("first CreateSelfSigned(): %v", err)
	}
	if _, err := CreateSelfSigned(ctx, database.Tag("master"), root, "ADMIN.LOCALHOST", time.Now()); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate CreateSelfSigned() error=%v, want ErrAlreadyExists", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(certificate root): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("certificate root entries=%d, want only the first persisted certificate", len(entries))
	}
}

func newCertificateTestDB(t *testing.T, ctx context.Context) orm.ORM {
	t.Helper()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "certificates.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	return database
}
