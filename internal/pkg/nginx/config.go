package nginx

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultConfigPath    = "/etc/nginx/conf.d/jasta.conf"
	DefaultChallengeRoot = "/var/lib/jasta/acme-challenges"
	DefaultPublicProxy   = "127.0.0.1:15432"
	DefaultAdminProxy    = "127.0.0.1:15433"
	DefaultNginxBinary   = "/usr/sbin/nginx"
	DefaultSystemctl     = "/usr/bin/systemctl"
)

type Site struct {
	Domains []string
}

type CertificatePaths struct {
	Certificate string
	PrivateKey  string
}

type Options struct {
	AdminDomain      string                      `yaml:"admin_domain"`
	PublicProxy      string                      `yaml:"public_proxy"`
	AdminProxy       string                      `yaml:"admin_proxy"`
	ChallengeRoot    string                      `yaml:"challenge_root"`
	AdminCertificate string                      `yaml:"admin_certificate"`
	AdminPrivateKey  string                      `yaml:"admin_private_key"`
	SiteCertificates map[string]CertificatePaths `yaml:"-"`
}

type Settings struct {
	AdminDomain      string                      `yaml:"admin_domain"`
	PublicProxy      string                      `yaml:"public_proxy"`
	AdminProxy       string                      `yaml:"admin_proxy"`
	ChallengeRoot    string                      `yaml:"challenge_root"`
	AdminCertificate string                      `yaml:"admin_certificate"`
	AdminPrivateKey  string                      `yaml:"admin_private_key"`
	ConfigPath       string                      `yaml:"config_path"`
	NginxBinary      string                      `yaml:"nginx_binary"`
	Systemctl        string                      `yaml:"systemctl_binary"`
	SiteCertificates map[string]CertificatePaths `yaml:"-"`
}

func (s Settings) RenderOptions() Options {
	return Options{
		AdminDomain: s.AdminDomain, PublicProxy: s.PublicProxy, AdminProxy: s.AdminProxy,
		ChallengeRoot: s.ChallengeRoot, AdminCertificate: s.AdminCertificate, AdminPrivateKey: s.AdminPrivateKey,
		SiteCertificates: s.SiteCertificates,
	}
}

func (s *Settings) SetDefaults() {
	if s.ConfigPath == "" {
		s.ConfigPath = DefaultConfigPath
	}
	if s.ChallengeRoot == "" {
		s.ChallengeRoot = DefaultChallengeRoot
	}
	if s.PublicProxy == "" {
		s.PublicProxy = DefaultPublicProxy
	}
	if s.AdminProxy == "" {
		s.AdminProxy = DefaultAdminProxy
	}
	if s.NginxBinary == "" {
		s.NginxBinary = DefaultNginxBinary
	}
	if s.Systemctl == "" {
		s.Systemctl = DefaultSystemctl
	}
}

func (s Settings) Validate() error {
	if s.AdminDomain == "" {
		return errors.New("nginx admin domain is required")
	}
	if _, err := normalizeDomain(s.AdminDomain); err != nil {
		return fmt.Errorf("invalid nginx admin domain: %w", err)
	}
	if err := validateLoopbackProxy(s.PublicProxy); err != nil {
		return fmt.Errorf("invalid nginx public proxy: %w", err)
	}
	if err := validateLoopbackProxy(s.AdminProxy); err != nil {
		return fmt.Errorf("invalid nginx admin proxy: %w", err)
	}
	for name, value := range map[string]string{"challenge root": s.ChallengeRoot, "config path": s.ConfigPath, "nginx binary": s.NginxBinary, "systemctl binary": s.Systemctl} {
		if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("nginx %s must be an absolute path", name)
		}
	}
	if (s.AdminCertificate == "") != (s.AdminPrivateKey == "") {
		return errors.New("nginx admin certificate and private key must be configured together")
	}
	for name, value := range map[string]string{"admin certificate": s.AdminCertificate, "admin private key": s.AdminPrivateKey} {
		if value != "" && (!filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n")) {
			return fmt.Errorf("nginx %s must be a clean absolute path", name)
		}
	}
	if filepath.Base(s.ConfigPath) != "jasta.conf" || filepath.Clean(s.ConfigPath) != s.ConfigPath {
		return errors.New("nginx config path must be a clean path ending in jasta.conf")
	}
	return nil
}

func validateLoopbackProxy(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return errors.New("must use an IP loopback address and port")
	}
	addr := net.ParseIP(host)
	if addr == nil || !addr.IsLoopback() {
		return errors.New("must use an IP loopback address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("must use a valid TCP port")
	}
	return nil
}
