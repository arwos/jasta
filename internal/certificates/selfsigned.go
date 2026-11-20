package certificates

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/orm"

	"go.arwos.org/jasta/internal/pkg/host"
)

const SelfSignedLifetime = 90 * 24 * time.Hour

var (
	ErrInvalidLocalDomain = errors.New("self-signed certificates are limited to local domains and private IPs")
	ErrAlreadyExists      = errors.New("certificate already exists for domain")
)

type Certificate struct {
	ID              string
	Domain          string
	Issuer          string
	Status          string
	CertificatePath string
	PrivateKeyPath  string
	NotBefore       time.Time
	NotAfter        time.Time
	CreatedAt       time.Time
}

func CreateSelfSigned(ctx context.Context, database orm.Stmt, root, rawDomain string, now time.Time) (result Certificate, err error) {
	if database == nil {
		return result, errors.New("certificate database is required")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return result, errors.New("certificate root must be a clean absolute path")
	}
	domain, err := localDomain(rawDomain)
	if err != nil {
		return result, err
	}
	if err = ensureDomainAvailable(ctx, database, domain); err != nil {
		return result, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC().Truncate(time.Second)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return result, fmt.Errorf("generate self-signed private key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return result, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	notBefore := now.Add(-5 * time.Minute)
	notAfter := now.Add(SelfSignedLifetime)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domain, Organization: []string{"Jasta Local"}},
		NotBefore:    notBefore, NotAfter: notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{domain}
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return result, fmt.Errorf("create self-signed certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return result, fmt.Errorf("encode self-signed private key: %w", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	id := uuid.NewString()
	if err = os.MkdirAll(root, 0o750); err != nil {
		return result, fmt.Errorf("create certificate root: %w", err)
	}
	temporary, err := os.MkdirTemp(root, ".jasta-certificate-*")
	if err != nil {
		return result, fmt.Errorf("create certificate staging directory: %w", err)
	}
	defer func() {
		if temporary != "" {
			_ = os.RemoveAll(temporary)
		}
	}()
	certificatePath := filepath.Join(temporary, "certificate.pem")
	privateKeyPath := filepath.Join(temporary, "private-key.pem")
	if err = writePrivateFile(certificatePath, certificatePEM, 0o644); err != nil {
		return result, fmt.Errorf("write self-signed certificate: %w", err)
	}
	if err = writePrivateFile(privateKeyPath, privateKeyPEM, 0o640); err != nil {
		return result, fmt.Errorf("write self-signed private key: %w", err)
	}
	// #nosec G302 -- Nginx needs group traversal to read the 0640 key; F07-T01 provisions the group.
	if err = os.Chmod(temporary, 0o750); err != nil {
		return result, fmt.Errorf("set certificate directory permissions: %w", err)
	}
	finalDirectory := filepath.Join(root, id)
	if err = os.Rename(temporary, finalDirectory); err != nil {
		return result, fmt.Errorf("install self-signed certificate files: %w", err)
	}
	temporary = ""
	result = Certificate{
		ID: id, Domain: domain, Issuer: "self_signed", Status: "valid",
		CertificatePath: filepath.Join(finalDirectory, "certificate.pem"), PrivateKeyPath: filepath.Join(finalDirectory, "private-key.pem"),
		NotBefore: notBefore, NotAfter: notAfter, CreatedAt: now,
	}
	if err = persist(ctx, database, result); err != nil {
		_ = os.RemoveAll(finalDirectory)
		if availableErr := ensureDomainAvailable(ctx, database, domain); errors.Is(availableErr, ErrAlreadyExists) {
			return Certificate{}, ErrAlreadyExists
		}
		return Certificate{}, err
	}
	return result, nil
}

func localDomain(raw string) (string, error) {
	if strings.Contains(raw, "*") || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: hostname must not include a port or wildcard", ErrInvalidLocalDomain)
	}
	ipValue := raw
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		ipValue = raw[1 : len(raw)-1]
	}
	if ip := net.ParseIP(ipValue); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return ip.String(), nil
		}
		return "", ErrInvalidLocalDomain
	}
	if strings.Contains(raw, ":") {
		return "", fmt.Errorf("%w: hostname must not include a port", ErrInvalidLocalDomain)
	}
	domain, err := host.Normalize(raw)
	if err != nil {
		return "", fmt.Errorf("%w: hostname is invalid", ErrInvalidLocalDomain)
	}
	if !strings.Contains(domain, ".") {
		return domain, nil
	}
	for _, suffix := range []string{".localhost", ".local", ".test", ".example", ".invalid", ".home.arpa"} {
		if strings.HasSuffix(domain, suffix) {
			return domain, nil
		}
	}
	return "", ErrInvalidLocalDomain
}

func ensureDomainAvailable(ctx context.Context, database orm.Stmt, domain string) error {
	return database.CallContext(ctx, "check existing certificate domain", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT id FROM certificates WHERE domain = ?", domain)
		if err != nil {
			return err
		}
		if rows.Next() {
			if err = rows.Close(); err != nil {
				return err
			}
			return ErrAlreadyExists
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
}

func writePrivateFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func persist(ctx context.Context, database orm.Stmt, certificate Certificate) error {
	err := database.CallContext(ctx, "store self-signed certificate metadata", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO certificates
			(id, domain, issuer, status, certificate_path, private_key_path, not_before, not_after, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, certificate.ID, certificate.Domain, certificate.Issuer, certificate.Status,
			certificate.CertificatePath, certificate.PrivateKeyPath, certificate.NotBefore.Unix(), certificate.NotAfter.Unix(),
			certificate.CreatedAt.Unix(), certificate.CreatedAt.Unix())
		return err
	})
	if err != nil {
		return fmt.Errorf("store certificate metadata: %w", err)
	}
	return nil
}
