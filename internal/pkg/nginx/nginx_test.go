package nginx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderCreatesIsolatedPublicAndAdminServerBlocks(t *testing.T) {
	content, err := Render([]Site{{Domains: []string{"WWW.Example.test", "example.test"}}}, Options{
		AdminDomain: "admin.example.test", PublicProxy: DefaultPublicProxy, AdminProxy: DefaultAdminProxy,
		ChallengeRoot: "/var/lib/jasta/acme-challenges",
	})
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"server_name example.test www.example.test;",
		"proxy_pass http://127.0.0.1:15432;",
		"server_name admin.example.test;",
		"return 308 https://$host$request_uri;",
		"location ^~ /.well-known/acme-challenge/",
		"root \"/var/lib/jasta/acme-challenges\";",
		"proxy_set_header X-Forwarded-Proto $scheme;",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered config does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Index(text, "location ^~ /.well-known/acme-challenge/") > strings.Index(text, "return 308") {
		t.Fatalf("HTTP-01 challenge does not precede admin redirect:\n%s", text)
	}
}

func TestRenderRejectsInvalidOrConflictingDomains(t *testing.T) {
	options := Options{AdminDomain: "admin.example.test"}
	for _, test := range []struct {
		name  string
		sites []Site
		admin string
	}{
		{name: "directive injection", sites: []Site{{Domains: []string{"good.test; include /tmp/evil"}}}},
		{name: "cross site duplicate", sites: []Site{{Domains: []string{"same.test"}}, {Domains: []string{"SAME.TEST"}}}},
		{name: "admin conflict", sites: []Site{{Domains: []string{"admin.example.test"}}}},
		{name: "port is rejected", sites: []Site{{Domains: []string{"example.test:8080"}}}},
		{name: "invalid admin host", sites: []Site{{Domains: []string{"www.example.test"}}}, admin: "admin.test;return 200"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.admin != "" {
				options.AdminDomain = test.admin
			} else {
				options.AdminDomain = "admin.example.test"
			}
			if _, err := Render(test.sites, options); err == nil {
				t.Fatal("Render() succeeded for invalid configuration")
			}
		})
	}
	if err := (Settings{AdminDomain: "admin.test", PublicProxy: "0.0.0.0:80"}).Validate(); err == nil {
		t.Fatal("Settings.Validate() accepted a non-loopback proxy")
	}
}

func TestRenderAddsAdminTLSProxyWhenCertificatePathsAreConfigured(t *testing.T) {
	content, err := Render(nil, Options{
		AdminDomain: "admin.example.test", AdminCertificate: "/var/lib/jasta/certs/admin/fullchain.pem",
		AdminPrivateKey: "/var/lib/jasta/certs/admin/private.key",
	})
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"listen 443 ssl;", `ssl_certificate "/var/lib/jasta/certs/admin/fullchain.pem";`,
		`ssl_certificate_key "/var/lib/jasta/certs/admin/private.key";`, "proxy_pass http://127.0.0.1:15433;",
		"proxy_set_header Upgrade $http_upgrade;", `proxy_set_header Connection "upgrade";`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered TLS config does not contain %q:\n%s", expected, text)
		}
	}
}

func TestApplyRunsCandidateAndFullValidationBeforeReload(t *testing.T) {
	settings := nginxTestSettings(t)
	content := []byte("server { listen 80; server_name example.test; }\n")
	runner := &recordingRunner{}
	if err := Apply(context.Background(), settings, content, runner); err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if len(runner.calls) != 3 || len(runner.calls[0]) != 4 ||
		!reflect.DeepEqual(runner.calls[0][:3], []string{DefaultNginxBinary, "-t", "-c"}) || !filepath.IsAbs(runner.calls[0][3]) ||
		!reflect.DeepEqual(runner.calls[1], []string{DefaultNginxBinary, "-t"}) ||
		!reflect.DeepEqual(runner.calls[2], []string{DefaultSystemctl, "reload", "nginx"}) {
		t.Fatalf("command calls = %#v", runner.calls)
	}
	installed, err := os.ReadFile(settings.ConfigPath)
	if err != nil {
		t.Fatalf("read installed config: %v", err)
	}
	if string(installed) != string(content) {
		t.Fatalf("installed config=%q, want %q", installed, content)
	}
	info, err := os.Stat(settings.ConfigPath)
	if err != nil {
		t.Fatalf("stat installed config: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("installed config mode=%v, want 0640", info.Mode().Perm())
	}
}

func TestApplyRestoresPreviousConfigWhenFullTestOrReloadFails(t *testing.T) {
	for _, failCall := range []int{2, 3} {
		t.Run(map[int]string{2: "test", 3: "reload"}[failCall], func(t *testing.T) {
			settings := nginxTestSettings(t)
			previous := []byte("server { listen 81; server_name old.test; }\n")
			if err := os.WriteFile(settings.ConfigPath, previous, 0o600); err != nil {
				t.Fatalf("seed existing config: %v", err)
			}
			runner := &recordingRunner{failAt: failCall}
			if err := Apply(context.Background(), settings, []byte("server { listen 80; server_name new.test; }\n"), runner); err == nil {
				t.Fatal("Apply() succeeded despite command failure")
			}
			actual, err := os.ReadFile(settings.ConfigPath)
			if err != nil {
				t.Fatalf("read restored config: %v", err)
			}
			if string(actual) != string(previous) {
				t.Fatalf("restored config=%q, want %q", actual, previous)
			}
			if failCall == 2 && len(runner.calls) != 2 {
				t.Fatalf("reload ran after failed full test: %#v", runner.calls)
			}
		})
	}
}

func TestApplyDoesNotInstallCandidateWhenIsolatedTestFails(t *testing.T) {
	settings := nginxTestSettings(t)
	runner := &recordingRunner{failAt: 1}
	if err := Apply(context.Background(), settings, []byte("broken"), runner); err == nil {
		t.Fatal("Apply() succeeded despite isolated test failure")
	}
	if _, err := os.Stat(settings.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate was installed before syntax test: stat err=%v", err)
	}
}

type recordingRunner struct {
	calls  [][]string
	failAt int
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if len(r.calls) == r.failAt {
		return errors.New("simulated command failure")
	}
	return nil
}

func nginxTestSettings(t *testing.T) Settings {
	t.Helper()
	directory := t.TempDir()
	return Settings{
		AdminDomain: "admin.example.test", ConfigPath: filepath.Join(directory, "jasta.conf"),
		ChallengeRoot: filepath.Join(directory, "challenge"), PublicProxy: DefaultPublicProxy, AdminProxy: DefaultAdminProxy,
		NginxBinary: DefaultNginxBinary, Systemctl: DefaultSystemctl,
	}
}
