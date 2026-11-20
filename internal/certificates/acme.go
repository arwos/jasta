package certificates

import (
	"bytes"
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
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"golang.org/x/crypto/acme"

	"go.arwos.org/jasta/internal/pkg/host"
)

const LetsEncryptLifetime = 90 * 24 * time.Hour

var (
	ErrInvalidPublicDomain = errors.New("let's encrypt requires a public DNS domain")
	ErrTermsNotAccepted    = errors.New("let's encrypt terms of service have not been accepted")
	challengeTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
	issuanceMu             sync.Mutex
)

type ACMEConfig struct {
	DirectoryURL string
	ContactEmail string
	AcceptTerms  bool
}

type ACMEClient interface {
	Register(context.Context, *acme.Account, func(string) bool) (*acme.Account, error)
	AuthorizeOrder(context.Context, []acme.AuthzID, ...acme.OrderOption) (*acme.Order, error)
	GetAuthorization(context.Context, string) (*acme.Authorization, error)
	HTTP01ChallengeResponse(string) (string, error)
	Accept(context.Context, *acme.Challenge) (*acme.Challenge, error)
	WaitAuthorization(context.Context, string) (*acme.Authorization, error)
	WaitOrder(context.Context, string) (*acme.Order, error)
	CreateCertFromOrder(context.Context, *acme.Order, []byte, bool) ([][]byte, string, error)
}

type ACMEClientFactory func(crypto.Signer) ACMEClient

func ValidateLetsEncryptDomain(raw string) (string, error) {
	if strings.Contains(raw, "*") || strings.Contains(raw, ":") || strings.TrimSpace(raw) != raw {
		return "", ErrInvalidPublicDomain
	}
	domain, err := host.Normalize(raw)
	if err != nil || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return "", ErrInvalidPublicDomain
	}
	for _, suffix := range []string{".localhost", ".local", ".test", ".example", ".invalid", ".home.arpa"} {
		if strings.HasSuffix(domain, suffix) {
			return "", ErrInvalidPublicDomain
		}
	}
	return domain, nil
}

func IssueLetsEncrypt(ctx context.Context, root, challengeRoot, rawDomain string, config ACMEConfig, factory ACMEClientFactory, progress func(string, int) error) (result Certificate, err error) {
	if !config.AcceptTerms {
		return result, ErrTermsNotAccepted
	}
	domain, err := ValidateLetsEncryptDomain(rawDomain)
	if err != nil {
		return result, err
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(challengeRoot) || filepath.Clean(challengeRoot) != challengeRoot {
		return result, errors.New("certificate and challenge roots must be clean absolute paths")
	}
	if err = validateDirectoryURL(config.DirectoryURL); err != nil {
		return result, err
	}
	issuanceMu.Lock()
	defer issuanceMu.Unlock()
	if err = notifyProgress(progress, "account", 10); err != nil {
		return result, err
	}
	key, err := loadOrCreateAccountKey(root)
	if err != nil {
		return result, err
	}
	if factory == nil {
		factory = func(signer crypto.Signer) ACMEClient {
			return &acme.Client{Key: signer, DirectoryURL: config.DirectoryURL, UserAgent: "Jasta"}
		}
	}
	client := factory(key)
	if client == nil {
		return result, errors.New("ACME client factory returned nil")
	}
	if err = registerAccount(ctx, client, root, config); err != nil {
		return result, err
	}
	if err = notifyProgress(progress, "challenge", 25); err != nil {
		return result, err
	}
	order, err := authorizeDomain(ctx, client, domain, challengeRoot)
	if err != nil {
		return result, err
	}
	if err = notifyProgress(progress, "certificate", 65); err != nil {
		return result, err
	}
	ready, err := client.WaitOrder(ctx, order.URI)
	if err != nil {
		return result, fmt.Errorf("wait for ACME order: %w", err)
	}
	if ready == nil || ready.Status != acme.StatusReady {
		return result, errors.New("ACME order did not become ready")
	}
	return installLetsEncryptCertificate(ctx, client, ready, root, domain)
}

func authorizeDomain(ctx context.Context, client ACMEClient, domain, challengeRoot string) (*acme.Order, error) {
	if err := os.MkdirAll(challengeRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create HTTP-01 challenge root: %w", err)
	}
	if err := os.Chmod(challengeRoot, 0o2750); err != nil {
		return nil, fmt.Errorf("set HTTP-01 challenge root permissions: %w", err)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("create ACME order: %w", err)
	}
	if order == nil || order.URI == "" {
		return nil, errors.New("ACME returned an invalid order")
	}
	challengeFiles := make([]string, 0, len(order.AuthzURLs))
	defer func() {
		for _, path := range challengeFiles {
			_ = os.Remove(path)
		}
	}()
	for _, authorizationURL := range order.AuthzURLs {
		if err = authorizeHTTP01(ctx, client, authorizationURL, domain, challengeRoot, &challengeFiles); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func authorizeHTTP01(ctx context.Context, client ACMEClient, authorizationURL, domain, challengeRoot string, challengeFiles *[]string) error {
	authorization, err := client.GetAuthorization(ctx, authorizationURL)
	if err != nil {
		return fmt.Errorf("load ACME authorization: %w", err)
	}
	if authorization == nil || authorization.URI == "" {
		return errors.New("ACME returned an invalid authorization")
	}
	if authorization.Wildcard || authorization.Identifier.Type != "dns" || authorization.Identifier.Value != domain {
		return ErrInvalidPublicDomain
	}
	if authorization.Status == acme.StatusValid {
		return nil
	}
	challenge := findHTTP01Challenge(authorization.Challenges)
	if challenge == nil || !challengeTokenPattern.MatchString(challenge.Token) {
		return errors.New("ACME authorization does not provide a valid HTTP-01 challenge")
	}
	response, err := client.HTTP01ChallengeResponse(challenge.Token)
	if err != nil {
		return fmt.Errorf("create HTTP-01 challenge response: %w", err)
	}
	if len(response) == 0 || len(response) > 1024 {
		return errors.New("HTTP-01 challenge response has an invalid length")
	}
	challengePath := filepath.Join(challengeRoot, ".well-known", "acme-challenge", challenge.Token)
	if err = os.MkdirAll(filepath.Dir(challengePath), 0o750); err != nil {
		return fmt.Errorf("create HTTP-01 challenge path: %w", err)
	}
	for _, directory := range []string{filepath.Join(challengeRoot, ".well-known"), filepath.Dir(challengePath)} {
		if err = os.Chmod(directory, 0o2750); err != nil {
			return fmt.Errorf("set HTTP-01 challenge directory permissions: %w", err)
		}
	}
	if err = writeChallengeFile(challengePath, []byte(response)); err != nil {
		return fmt.Errorf("publish HTTP-01 challenge: %w", err)
	}
	*challengeFiles = append(*challengeFiles, challengePath)
	if _, err = client.Accept(ctx, challenge); err != nil {
		return fmt.Errorf("accept HTTP-01 challenge: %w", err)
	}
	validated, err := client.WaitAuthorization(ctx, authorization.URI)
	if err != nil {
		return fmt.Errorf("HTTP-01 challenge validation failed: %w", err)
	}
	if validated == nil || validated.Status != acme.StatusValid {
		return errors.New("HTTP-01 challenge was not validated")
	}
	return nil
}

func findHTTP01Challenge(challenges []*acme.Challenge) *acme.Challenge {
	for _, challenge := range challenges {
		if challenge != nil && challenge.Type == "http-01" {
			return challenge
		}
	}
	return nil
}

func installLetsEncryptCertificate(ctx context.Context, client ACMEClient, ready *acme.Order, root, domain string) (Certificate, error) {
	certificateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, fmt.Errorf("generate certificate private key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
	}, certificateKey)
	if err != nil {
		return Certificate{}, fmt.Errorf("create certificate request: %w", err)
	}
	chainDER, _, err := client.CreateCertFromOrder(ctx, ready, csrDER, true)
	if err != nil {
		return Certificate{}, fmt.Errorf("finalize ACME order: %w", err)
	}
	if len(chainDER) == 0 {
		return Certificate{}, errors.New("ACME returned an empty certificate chain")
	}
	leaf, err := x509.ParseCertificate(chainDER[0])
	if err != nil {
		return Certificate{}, fmt.Errorf("parse ACME leaf certificate: %w", err)
	}
	if err = leaf.VerifyHostname(domain); err != nil {
		return Certificate{}, fmt.Errorf("ACME certificate does not cover requested domain: %w", err)
	}
	leafPublicKey, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		return Certificate{}, fmt.Errorf("encode ACME leaf public key: %w", err)
	}
	requestedPublicKey, err := x509.MarshalPKIXPublicKey(&certificateKey.PublicKey)
	if err != nil {
		return Certificate{}, fmt.Errorf("encode requested public key: %w", err)
	}
	if !bytes.Equal(leafPublicKey, requestedPublicKey) {
		return Certificate{}, errors.New("ACME certificate does not match the generated private key")
	}
	now := time.Now()
	if !leaf.NotAfter.After(leaf.NotBefore) || leaf.NotBefore.After(now.Add(5*time.Minute)) || now.After(leaf.NotAfter) || leaf.NotAfter.Sub(leaf.NotBefore) > LetsEncryptLifetime+24*time.Hour {
		return Certificate{}, errors.New("ACME returned a certificate with an invalid validity period")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(certificateKey)
	if err != nil {
		return Certificate{}, fmt.Errorf("encode certificate private key: %w", err)
	}
	chainPEM := make([]byte, 0, 4096)
	for _, der := range chainDER {
		chainPEM = append(chainPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	id := uuid.NewString()
	if err = os.MkdirAll(root, 0o750); err != nil {
		return Certificate{}, fmt.Errorf("create certificate root: %w", err)
	}
	stagingDir, err := os.MkdirTemp(root, ".jasta-acme-*")
	if err != nil {
		return Certificate{}, fmt.Errorf("create ACME certificate staging directory: %w", err)
	}
	defer func() {
		if stagingDir != "" {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	if err = writePrivateFile(filepath.Join(stagingDir, "certificate.pem"), chainPEM, 0o644); err != nil {
		return Certificate{}, fmt.Errorf("write ACME certificate chain: %w", err)
	}
	if err = writePrivateFile(filepath.Join(stagingDir, "private-key.pem"), keyPEM, 0o640); err != nil {
		return Certificate{}, fmt.Errorf("write ACME private key: %w", err)
	}
	// #nosec G302 -- Nginx needs group traversal; F07-T01 provisions the shared group.
	if err = os.Chmod(stagingDir, 0o750); err != nil {
		return Certificate{}, fmt.Errorf("set ACME certificate directory permissions: %w", err)
	}
	finalDir := filepath.Join(root, id)
	if err = os.Rename(stagingDir, finalDir); err != nil {
		return Certificate{}, fmt.Errorf("install ACME certificate files: %w", err)
	}
	stagingDir = ""
	return Certificate{ID: id, Domain: domain, Issuer: "letsencrypt", Status: "valid", CertificatePath: filepath.Join(finalDir, "certificate.pem"),
		PrivateKeyPath: filepath.Join(finalDir, "private-key.pem"), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, CreatedAt: time.Now().UTC()}, nil
}

func validateDirectoryURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("ACME directory URL must be an absolute HTTPS URL")
	}
	return nil
}

func loadOrCreateAccountKey(root string) (*ecdsa.PrivateKey, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create ACME account storage: %w", err)
	}
	path := filepath.Join(root, "acme-account-key.pem")
	info, statErr := os.Lstat(path)
	if statErr == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0) {
		return nil, errors.New("ACME account key file must be a regular file with mode 0600 or stricter")
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect ACME account key: %w", statErr)
	}
	content, err := os.ReadFile(path)
	if err == nil {
		block, rest := pem.Decode(content)
		if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
			return nil, errors.New("ACME account key file is invalid")
		}
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse ACME account key: %w", parseErr)
		}
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok || key.Curve != elliptic.P256() {
			return nil, errors.New("ACME account key must be ECDSA P-256")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read ACME account key: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ACME account key: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encode ACME account key: %w", err)
	}
	content = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create ACME account key: %w", err)
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write ACME account key: %w", err)
	}
	return key, nil
}

func registerAccount(ctx context.Context, client ACMEClient, root string, config ACMEConfig) error {
	uriPath := filepath.Join(root, "acme-account-uri")
	if content, err := os.ReadFile(uriPath); err == nil {
		uri := strings.TrimSpace(string(content))
		if uri == "" || strings.ContainsAny(uri, "\r\n") {
			return errors.New("ACME account URI file is invalid")
		}
		// The production client is the only implementation used by this helper.
		if production, ok := client.(*acme.Client); ok {
			production.KID = acme.KeyID(uri)
			return nil
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read ACME account URI: %w", err)
	}
	contact := []string(nil)
	if config.ContactEmail != "" {
		contact = []string{"mailto:" + config.ContactEmail}
	}
	account, err := client.Register(ctx, &acme.Account{Contact: contact}, func(string) bool { return config.AcceptTerms })
	if err != nil {
		return fmt.Errorf("register ACME account: %w", err)
	}
	if account == nil || account.URI == "" {
		return errors.New("ACME registration returned no account URI")
	}
	if err = writeAtomic(uriPath, []byte(account.URI+"\n"), 0o600); err != nil {
		return fmt.Errorf("save ACME account URI: %w", err)
	}
	if production, ok := client.(*acme.Client); ok {
		production.KID = acme.KeyID(account.URI)
	}
	return nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jasta-acme-account-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(content)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func writeChallengeFile(path string, content []byte) error {
	// #nosec G302 -- Nginx must serve the HTTP-01 response without private content.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err = file.Chmod(0o644); err == nil {
		_, err = file.Write(content)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func notifyProgress(progress func(string, int) error, stage string, value int) error {
	if progress == nil {
		return nil
	}
	return progress(stage, value)
}

// ReplaceIssuedCertificate saves a new certificate record and returns the
// previous record so callers can restore it if Nginx refuses the new files.
func ReplaceIssuedCertificate(ctx context.Context, database orm.Stmt, certificate Certificate) (*Certificate, error) {
	var previous *Certificate
	err := database.CallContext(ctx, "replace issued certificate metadata", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, `SELECT id, domain, issuer, status, certificate_path, private_key_path,
			not_before, not_after, created_at FROM certificates WHERE domain = ?`, certificate.Domain)
		if err != nil {
			return err
		}
		if rows.Next() {
			var item Certificate
			var notBefore, notAfter, createdAt sql.NullInt64
			if err = rows.Scan(&item.ID, &item.Domain, &item.Issuer, &item.Status, &item.CertificatePath, &item.PrivateKeyPath, &notBefore, &notAfter, &createdAt); err != nil {
				_ = rows.Close()
				return err
			}
			if notBefore.Valid {
				item.NotBefore = time.Unix(notBefore.Int64, 0).UTC()
			}
			if notAfter.Valid {
				item.NotAfter = time.Unix(notAfter.Int64, 0).UTC()
			}
			if createdAt.Valid {
				item.CreatedAt = time.Unix(createdAt.Int64, 0).UTC()
			}
			previous = &item
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO certificates
			(id, domain, issuer, status, certificate_path, private_key_path, not_before, not_after, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(domain) DO UPDATE SET issuer = excluded.issuer, status = excluded.status,
			certificate_path = excluded.certificate_path, private_key_path = excluded.private_key_path,
			not_before = excluded.not_before, not_after = excluded.not_after, updated_at = excluded.updated_at`,
			certificate.ID, certificate.Domain, certificate.Issuer, certificate.Status, certificate.CertificatePath, certificate.PrivateKeyPath,
			certificate.NotBefore.Unix(), certificate.NotAfter.Unix(), certificate.CreatedAt.Unix(), certificate.CreatedAt.Unix())
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("replace certificate metadata: %w", err)
	}
	return previous, nil
}

func RestoreIssuedCertificate(ctx context.Context, database orm.Stmt, issued Certificate, previous *Certificate) error {
	return database.CallContext(ctx, "restore previous certificate metadata", func(ctx context.Context, db orm.DB) error {
		if previous == nil {
			_, err := db.ExecContext(ctx, "DELETE FROM certificates WHERE domain = ? AND id = ?", issued.Domain, issued.ID)
			return err
		}
		_, err := db.ExecContext(ctx, `UPDATE certificates SET issuer = ?, status = ?, certificate_path = ?, private_key_path = ?,
			not_before = ?, not_after = ?, created_at = ?, updated_at = ? WHERE domain = ?`, previous.Issuer, previous.Status,
			previous.CertificatePath, previous.PrivateKeyPath, nullableUnix(previous.NotBefore), nullableUnix(previous.NotAfter),
			previous.CreatedAt.Unix(), previous.CreatedAt.Unix(), previous.Domain)
		return err
	})
}

func nullableUnix(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.Unix()
}
