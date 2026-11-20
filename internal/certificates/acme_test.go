package certificates

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"golang.org/x/crypto/acme"
)

func TestValidateLetsEncryptDomain(t *testing.T) {
	for _, test := range []struct {
		domain string
		valid  bool
	}{
		{domain: "Example.COM.", valid: true},
		{domain: "example.com", valid: true},
		{domain: "localhost", valid: false},
		{domain: "site.local", valid: false},
		{domain: "192.0.2.1", valid: false},
		{domain: "*.example.com", valid: false},
		{domain: "example.com:443", valid: false},
	} {
		_, err := ValidateLetsEncryptDomain(test.domain)
		if (err == nil) != test.valid {
			t.Errorf("ValidateLetsEncryptDomain(%q) error=%v, valid=%t", test.domain, err, test.valid)
		}
	}
}

func TestIssueLetsEncryptPublishesChallengeAndStoresCandidateSeparately(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "certificates")
	challengeRoot := filepath.Join(t.TempDir(), "challenges")
	client := &fakeACMEClient{challengePath: filepath.Join(challengeRoot, ".well-known", "acme-challenge", "challenge-token")}
	var progress []int
	result, err := IssueLetsEncrypt(ctx, root, challengeRoot, "WWW.Example.com.", ACMEConfig{
		DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory", AcceptTerms: true,
	}, func(crypto.Signer) ACMEClient { return client }, func(_ string, value int) error {
		progress = append(progress, value)
		return nil
	})
	if err != nil {
		t.Fatalf("IssueLetsEncrypt(): %v", err)
	}
	if result.Domain != "www.example.com" || result.Issuer != "letsencrypt" || result.Status != "valid" || !client.challengeObserved {
		t.Fatalf("certificate=%#v challengeObserved=%t", result, client.challengeObserved)
	}
	if _, err = os.Stat(client.challengePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("challenge file remains after issuance: %v", err)
	}
	if len(progress) < 3 || progress[len(progress)-1] != 65 {
		t.Fatalf("progress callbacks=%v", progress)
	}
	keyInfo, err := os.Stat(result.PrivateKeyPath)
	if err != nil || keyInfo.Mode().Perm() != 0o640 {
		t.Fatalf("private key stat=%v err=%v", keyInfo, err)
	}
	certificatePEM, err := os.ReadFile(result.CertificatePath)
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}
	block, _ := pem.Decode(certificatePEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || leaf.VerifyHostname("www.example.com") != nil {
		t.Fatalf("ACME leaf certificate=%v err=%v", leaf, err)
	}
}

func TestIssueLetsEncryptRejectsTermsAndLocalNamesBeforeCreatingFiles(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "certificates")
	config := ACMEConfig{DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory"}
	if _, err := IssueLetsEncrypt(ctx, root, filepath.Join(t.TempDir(), "challenge"), "example.com", config, nil, nil); !errors.Is(err, ErrTermsNotAccepted) {
		t.Fatalf("terms-unaccepted error=%v", err)
	}
	config.AcceptTerms = true
	if _, err := IssueLetsEncrypt(ctx, root, filepath.Join(t.TempDir(), "challenge"), "service.local", config, nil, nil); !errors.Is(err, ErrInvalidPublicDomain) {
		t.Fatalf("local domain error=%v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected request created certificate files: %v", err)
	}
}

func TestReplaceIssuedCertificateCanRestorePreviousCertificate(t *testing.T) {
	ctx := context.Background()
	database := newCertificateTestDB(t, ctx)
	now := time.Now().UTC().Truncate(time.Second)
	previous := Certificate{ID: "previous", Domain: "www.example.com", Issuer: "self_signed", Status: "valid",
		CertificatePath: "/certs/previous/cert.pem", PrivateKeyPath: "/certs/previous/key.pem", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour)}
	if _, err := ReplaceIssuedCertificate(ctx, database.Tag("master"), previous); err != nil {
		t.Fatalf("initial ReplaceIssuedCertificate(): %v", err)
	}
	issued := Certificate{ID: "issued", Domain: previous.Domain, Issuer: "letsencrypt", Status: "valid",
		CertificatePath: "/certs/issued/cert.pem", PrivateKeyPath: "/certs/issued/key.pem", NotBefore: now, NotAfter: now.Add(LetsEncryptLifetime), CreatedAt: now}
	storedPrevious, err := ReplaceIssuedCertificate(ctx, database.Tag("master"), issued)
	if err != nil {
		t.Fatalf("ReplaceIssuedCertificate(): %v", err)
	}
	if storedPrevious == nil || storedPrevious.ID != previous.ID || storedPrevious.CertificatePath != previous.CertificatePath {
		t.Fatalf("previous certificate metadata=%#v", storedPrevious)
	}
	if err = RestoreIssuedCertificate(ctx, database.Tag("master"), issued, storedPrevious); err != nil {
		t.Fatalf("RestoreIssuedCertificate(): %v", err)
	}
	var issuer, path string
	if err = database.Tag("master").CallContext(ctx, "verify restored certificate", func(ctx context.Context, db orm.DB) error {
		rows, queryErr := db.QueryContext(ctx, "SELECT issuer, certificate_path FROM certificates WHERE domain = ?", previous.Domain)
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return sql.ErrNoRows
		}
		return rows.Scan(&issuer, &path)
	}); err != nil {
		t.Fatalf("read restored certificate: %v", err)
	}
	if issuer != previous.Issuer || path != previous.CertificatePath {
		t.Fatalf("restored metadata issuer=%q path=%q", issuer, path)
	}
}

type fakeACMEClient struct {
	challengePath     string
	challengeObserved bool
}

func (f *fakeACMEClient) Register(context.Context, *acme.Account, func(string) bool) (*acme.Account, error) {
	return &acme.Account{URI: "https://acme.example.test/account/1"}, nil
}

func (f *fakeACMEClient) AuthorizeOrder(context.Context, []acme.AuthzID, ...acme.OrderOption) (*acme.Order, error) {
	return &acme.Order{URI: "https://acme.example.test/order/1", AuthzURLs: []string{"https://acme.example.test/authz/1"}}, nil
}

func (f *fakeACMEClient) GetAuthorization(context.Context, string) (*acme.Authorization, error) {
	return &acme.Authorization{URI: "https://acme.example.test/authz/1", Status: acme.StatusPending,
		Identifier: acme.AuthzID{Type: "dns", Value: "www.example.com"},
		Challenges: []*acme.Challenge{{Type: "http-01", Token: "challenge-token", URI: "https://acme.example.test/challenge/1"}}}, nil
}

func (*fakeACMEClient) HTTP01ChallengeResponse(string) (string, error) {
	return "token.thumbprint", nil
}
func (*fakeACMEClient) Accept(context.Context, *acme.Challenge) (*acme.Challenge, error) {
	return &acme.Challenge{Status: acme.StatusProcessing}, nil
}

func (f *fakeACMEClient) WaitAuthorization(context.Context, string) (*acme.Authorization, error) {
	content, err := os.ReadFile(f.challengePath)
	if err != nil || string(content) != "token.thumbprint" {
		return nil, errors.New("challenge content was not published at Nginx root path")
	}
	f.challengeObserved = true
	return &acme.Authorization{Status: acme.StatusValid}, nil
}

func (*fakeACMEClient) WaitOrder(context.Context, string) (*acme.Order, error) {
	return &acme.Order{Status: acme.StatusReady}, nil
}

func (*fakeACMEClient) CreateCertFromOrder(_ context.Context, _ *acme.Order, csrDER []byte, _ bool) ([][]byte, string, error) {
	request, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, "", err
	}
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "www.example.com"},
		DNSNames: []string{"www.example.com"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(LetsEncryptLifetime),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, request.PublicKey, signer)
	if err != nil {
		return nil, "", err
	}
	return [][]byte{certificateDER}, "https://acme.example.test/cert/1", nil
}
