package archive

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

type pipeAddr string

func (a pipeAddr) Network() string { return "tcp" }
func (a pipeAddr) String() string  { return string(a) }

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr("registry.example.com:443") }

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestParseExternalURL(t *testing.T) {
	valid := []string{
		"https://registry.example/releases/site.zip?signature=secret",
		"HTTPS://registry.example:8443/path",
		"https://93.184.216.34/archive.tar.gz",
		"https://[2606:4700:4700::1111]/archive.zip",
	}
	for _, raw := range valid {
		if _, err := parseExternalURL(raw); err != nil {
			t.Errorf("parseExternalURL(%q): %v", raw, err)
		}
	}
	invalid := []string{
		"http://registry.example/file.zip",
		"https://user:password@registry.example/file.zip",
		"https://registry.example/file.zip#fragment",
		"https://registry.example:99999/file.zip",
		"https://127.1/file.zip",
		"https://2130706433/file.zip",
		"https://0x7f000001/file.zip",
		"https://0o177.1/file.zip",
		"https://registry.example./file.zip",
		"https://[fe80::1%25eth0]/file.zip",
	}
	for _, raw := range invalid {
		if _, err := parseExternalURL(raw); err == nil {
			t.Errorf("parseExternalURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestIsExternalIPRejectsSpecialAndMappedDestinations(t *testing.T) {
	for _, raw := range []string{
		"0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
		"172.16.0.1", "192.0.0.8", "192.0.0.170", "192.0.2.1", "192.168.1.1", "198.18.0.1",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2001:2::1",
		"2002::1", "3fff::1", "64:ff9b:1::1", "64:ff9b::a00:1",
	} {
		address := netip.MustParseAddr(raw)
		if isExternalIP(address) {
			t.Errorf("isExternalIP(%s) unexpectedly allowed", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "93.184.216.34", "192.0.0.9", "192.0.0.10", "2001:1::1", "2001:3::1", "2001:20::1", "64:ff9b::808:808", "2606:4700:4700::1111"} {
		if address := netip.MustParseAddr(raw); !isExternalIP(address) {
			t.Errorf("isExternalIP(%s) unexpectedly blocked", raw)
		}
	}
}

func TestValidateExternalHostRejectsAnyProhibitedDNSAnswer(t *testing.T) {
	for _, answers := range [][]netip.Addr{
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::ffff:10.0.0.1")},
	} {
		resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return answers, nil })
		if err := validateExternalHost(context.Background(), "registry.example.com", resolver); err == nil {
			t.Fatalf("answers %v unexpectedly passed", answers)
		}
	}
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	if err := validateExternalHost(context.Background(), "registry.example.com", resolver); err != nil {
		t.Fatalf("public DNS answer rejected: %v", err)
	}
	if err := validateExternalHost(context.Background(), "local", resolver); err == nil {
		t.Fatal("local-only hostname was accepted")
	}
}

func TestFetchPinsRevalidatedDNSAndRedactsSignedURL(t *testing.T) {
	lookups := 0
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if err := validateExternalHost(context.Background(), "registry.example.com", resolver); err != nil {
		t.Fatal(err)
	}
	dialed := false
	transport := newFetchTransport(resolver, func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected dial")
	})
	_, err := transport.DialContext(context.Background(), "tcp", "registry.example.com:443")
	if err == nil || dialed {
		t.Fatalf("DNS rebinding was not blocked before connect: err=%v dialed=%t", err, dialed)
	}

	publicResolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	_, err = fetch(context.Background(), Source{URL: "https://registry.example.com/site.zip?token=private-value", AuthType: AuthQuery}, FetchOptions{Limits: Limits{MaxArchiveBytes: 1024, MaxUnpacked: 2048, MaxEntries: 2}}, publicResolver, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("safe fetch failure category was not retained: %v", err)
	}
	if strings.Contains(err.Error(), "private-value") {
		t.Fatalf("signed URL leaked in error: %v", err)
	}
}

func TestRedirectPolicyRevalidatesTargetAndStripsCredentials(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "private.example.org" {
			return []netip.Addr{netip.MustParseAddr("10.0.0.7")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	policy := redirectPolicy(resolver)
	previousURL, _ := url.Parse("https://registry.example.com/start")
	previous := &http.Request{URL: previousURL}
	via := []*http.Request{previous}

	nextURL, _ := url.Parse("https://cdn.example.net/object")
	next := &http.Request{URL: nextURL, Header: http.Header{"Authorization": []string{"Bearer secret"}, "Cookie": []string{"session=secret"}}}
	if err := policy(next, via); err != nil {
		t.Fatal(err)
	}
	if next.Header.Get("Authorization") != "" || next.Header.Get("Cookie") != "" {
		t.Fatal("credentials copied to another authority")
	}

	blockedURL, _ := url.Parse("https://private.example.org/metadata")
	blocked := &http.Request{URL: blockedURL, Header: make(http.Header)}
	if err := policy(blocked, via); err == nil {
		t.Fatal("redirect to private DNS answer succeeded")
	}

	sameURL, _ := url.Parse("https://registry.example.com/next")
	same := &http.Request{URL: sameURL, Header: http.Header{"Authorization": []string{"Bearer secret"}}}
	if err := policy(same, via); err != nil {
		t.Fatal(err)
	}
	if same.Header.Get("Authorization") != "Bearer secret" {
		t.Fatal("same-authority redirect lost authorization")
	}
}

func TestCredentialsAndBoundedResponseBody(t *testing.T) {
	basic, _ := http.NewRequest(http.MethodGet, "https://registry.example.com/", nil)
	applyCredentials(basic, Source{AuthType: AuthBasic, Username: "deploy", Password: "secret"})
	username, password, ok := basic.BasicAuth()
	if !ok || username != "deploy" || password != "secret" {
		t.Fatal("Basic credentials were not set")
	}
	bearer, _ := http.NewRequest(http.MethodGet, "https://registry.example.com/", nil)
	applyCredentials(bearer, Source{AuthType: AuthBearer, BearerToken: "secret-token"})
	if bearer.Header.Get("Authorization") != "Bearer secret-token" {
		t.Fatal("Bearer token was not set")
	}
	if err := validateCredentials(Source{AuthType: AuthBearer, Username: "mixed", BearerToken: "token"}); err == nil {
		t.Fatal("mixed auth credentials were accepted")
	}
	if err := validateCredentials(Source{AuthType: AuthBasic, Username: "user:admin", Password: "password"}); err == nil {
		t.Fatal("Basic username containing a colon was accepted")
	}
	if err := validateCredentials(Source{AuthType: AuthBearer, BearerToken: "token\r\nInjected: value"}); err == nil {
		t.Fatal("Bearer token containing header delimiters was accepted")
	}
	if err := validateCredentials(Source{AuthType: AuthBearer, BearerToken: "==="}); err == nil {
		t.Fatal("Bearer padding without token data was accepted")
	}
	encoded, err := json.Marshal(Source{URL: "https://registry.example.com/?token=private", AuthType: AuthBasic, Username: "user", Password: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{}` {
		t.Fatalf("source unexpectedly serializes secrets: %s", encoded)
	}

	body := &boundedResponseBody{body: io.NopCloser(strings.NewReader("12345")), limit: 4, cancel: func() {}}
	data, err := io.ReadAll(body)
	if !errors.Is(err, ErrArchiveTooLarge) || string(data) != "12345" {
		t.Fatalf("limit crossing not reported: data=%q err=%v", data, err)
	}
	_ = body.Close()
}

func TestFetchSendsAuthAndStripsItAcrossRedirects(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	var requests int
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if request.URL.RawQuery != "signature=private" {
				t.Errorf("signed query changed: %q", request.URL.RawQuery)
			}
			username, password, ok := request.BasicAuth()
			if !ok || username != "publisher" || password != "secret" {
				t.Errorf("Basic auth missing on registry request: %v", request.Header)
			}
			return testResponse(request, http.StatusFound, http.Header{"Location": []string{"https://cdn.example.net/archive.zip?signature=private"}}, ""), nil
		}
		if request.URL.Hostname() != "cdn.example.net" {
			t.Errorf("redirect host = %q", request.URL.Hostname())
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("credentials leaked to redirected host: %v", request.Header)
		}
		return testResponse(request, http.StatusOK, make(http.Header), "archive"), nil
	})
	body, err := fetchWithTransport(context.Background(), Source{URL: "https://registry.example.com/archive.zip?signature=private", AuthType: AuthBasic, Username: "publisher", Password: "secret"}, FetchOptions{Limits: Limits{MaxArchiveBytes: 64, MaxUnpacked: 128, MaxEntries: 2}}, resolver, transport)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "archive" || requests != 2 {
		t.Fatalf("body=%q requests=%d", data, requests)
	}
	_ = body.Close()
}

func TestFetchHTTPSWithPinnedPublicDNSAndRegistryAuth(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "registry.example.com"},
		DNSNames:     []string{"registry.example.com", "cdn.example.net"},
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	serverCertificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: certificate}

	listener := &pipeListener{connections: make(chan net.Conn, 4), done: make(chan struct{})}
	var requests atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/bearer" {
			if request.Header.Get("Authorization") != "Bearer deploy-token" {
				t.Errorf("missing registry Bearer auth")
			}
			_, _ = io.WriteString(w, "bearer archive")
			return
		}
		if request.URL.Path == "/start" {
			user, password, ok := request.BasicAuth()
			if !ok || user != "publisher" || password != "secret" {
				t.Errorf("missing registry Basic auth")
			}
			if request.URL.RawQuery != "signature=initial-secret" {
				t.Errorf("initial query = %q", request.URL.RawQuery)
			}
			http.Redirect(w, request, "https://cdn.example.net/download?signature=redirect-secret", http.StatusFound)
			return
		}
		if request.URL.Path != "/download" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("Authorization leaked across redirect")
		}
		if request.URL.RawQuery != "signature=redirect-secret" {
			t.Errorf("redirect query = %q", request.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "archive bytes")
	})
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	resolver := resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "registry.example.com" && host != "cdn.example.net" {
			t.Errorf("unexpected lookup host %q", host)
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		tlsServerConn := tls.Server(serverConn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverCertificate}})
		select {
		case listener.connections <- tlsServerConn:
			return clientConn, nil
		case <-ctx.Done():
			_ = clientConn.Close()
			_ = tlsServerConn.Close()
			return nil, ctx.Err()
		case <-listener.done:
			_ = clientConn.Close()
			_ = tlsServerConn.Close()
			return nil, net.ErrClosed
		}
	}
	transport := newFetchTransportWithRoots(resolver, dial, roots)
	body, err := fetchWithTransport(context.Background(), Source{
		URL:      "https://registry.example.com/start?signature=initial-secret",
		AuthType: AuthBasic, Username: "publisher", Password: "secret",
	}, FetchOptions{Limits: Limits{MaxArchiveBytes: 64, MaxUnpacked: 128, MaxEntries: 2}, Timeout: time.Second}, resolver, transport)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "archive bytes" || requests.Load() != 2 {
		t.Fatalf("body=%q requests=%d", data, requests.Load())
	}
	_ = body.Close()
	bearerBody, err := fetchWithTransport(context.Background(), Source{
		URL: "https://registry.example.com/bearer", AuthType: AuthBearer, BearerToken: "deploy-token",
	}, FetchOptions{Limits: Limits{MaxArchiveBytes: 64, MaxUnpacked: 128, MaxEntries: 2}, Timeout: time.Second}, resolver, newFetchTransportWithRoots(resolver, dial, roots))
	if err != nil {
		t.Fatal(err)
	}
	bearerData, err := io.ReadAll(bearerBody)
	_ = bearerBody.Close()
	if err != nil || string(bearerData) != "bearer archive" || requests.Load() != 3 {
		t.Fatalf("bearer body=%q requests=%d err=%v", bearerData, requests.Load(), err)
	}
}

func TestFetchSendsBearerToken(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer deploy-token" {
			t.Errorf("Authorization = %q", got)
		}
		return testResponse(request, http.StatusOK, make(http.Header), "archive"), nil
	})
	body, err := fetchWithTransport(context.Background(), Source{URL: "https://registry.example.com/archive.zip", AuthType: AuthBearer, BearerToken: "deploy-token"}, FetchOptions{Limits: Limits{MaxArchiveBytes: 64, MaxUnpacked: 128, MaxEntries: 2}}, resolver, transport)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if data, err := io.ReadAll(body); err != nil || string(data) != "archive" {
		t.Fatalf("body=%q err=%v", data, err)
	}
}

func TestFetchBlocksRedirectToLocalHostname(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return testResponse(request, http.StatusFound, http.Header{"Location": []string{"https://metadata.internal/latest"}}, ""), nil
	})
	_, err := fetchWithTransport(context.Background(), Source{URL: "https://registry.example.com/archive.zip?signature=private", AuthType: AuthQuery}, FetchOptions{Limits: Limits{MaxArchiveBytes: 64, MaxUnpacked: 128, MaxEntries: 2}}, resolver, transport)
	if err == nil || requests != 1 {
		t.Fatalf("private redirect result err=%v requests=%d", err, requests)
	}
	if strings.Contains(err.Error(), "signature=private") {
		t.Fatalf("signed URL leaked through redirect error: %v", err)
	}
}

func testResponse(request *http.Request, status int, header http.Header, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}
