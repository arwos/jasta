package archive

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AuthType string

const (
	AuthNone   AuthType = "none"
	AuthBasic  AuthType = "basic"
	AuthBearer AuthType = "bearer"
	AuthQuery  AuthType = "query"
)

// Source contains an HTTPS archive URL and credentials for its registry.
// Credentials are deliberately not serializable; callers must retrieve them
// from the encrypted registry store and must not log this value.
type Source struct {
	URL         string   `json:"-" yaml:"-"`
	AuthType    AuthType `json:"-" yaml:"-"`
	Username    string   `json:"-" yaml:"-"`
	Password    string   `json:"-" yaml:"-"`
	BearerToken string   `json:"-" yaml:"-"`
}

type FetchOptions struct {
	Limits  Limits
	Timeout time.Duration
}

type ipResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type dialFunc func(context.Context, string, string) (net.Conn, error)

type fetchFailure struct {
	message string
	cause   error
}

var ErrFetchFailed = errors.New("archive request failed")

func (e *fetchFailure) Error() string { return e.message }
func (e *fetchFailure) Unwrap() error { return e.cause }

// Fetch opens an archive from an external HTTPS endpoint. The returned body
// is streamed and enforces the configured compressed-input limit. The caller
// must close it; archive.Extract can consume it directly.
func Fetch(ctx context.Context, source Source, options FetchOptions) (io.ReadCloser, error) {
	return fetch(ctx, source, options, net.DefaultResolver, nil)
}

// ValidateSource validates an archive source without resolving or connecting
// to its host. Public-IP validation is performed by Fetch immediately before
// each connection and after redirects.
func ValidateSource(source Source) error {
	requestURL, err := parseExternalURL(source.URL)
	if err != nil {
		return err
	}
	if err = validateCredentials(source); err != nil {
		return err
	}
	if source.AuthType == AuthQuery && requestURL.RawQuery == "" {
		return errors.New("query auth requires a signed query string")
	}
	return nil
}

func fetch(ctx context.Context, source Source, options FetchOptions, resolver ipResolver, dialOverride dialFunc) (io.ReadCloser, error) {
	return fetchWithTransport(ctx, source, options, resolver, newFetchTransport(resolver, dialOverride))
}

func fetchWithTransport(ctx context.Context, source Source, options FetchOptions, resolver ipResolver, transport http.RoundTripper) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("fetch context is required")
	}
	if resolver == nil {
		return nil, errors.New("DNS resolver is required")
	}
	limits, err := options.Limits.withDefaults()
	if err != nil {
		return nil, err
	}
	if limits.MaxArchiveBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("archive input limit is too large")
	}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Minute
	}
	requestCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	requestURL, err := parseExternalURL(source.URL)
	if err != nil {
		cancel()
		return nil, err
	}
	if err = validateExternalHost(requestCtx, requestURL.Hostname(), resolver); err != nil {
		cancel()
		return nil, err
	}
	if err = validateCredentials(source); err != nil {
		cancel()
		return nil, err
	}
	if source.AuthType == AuthQuery && requestURL.RawQuery == "" {
		cancel()
		return nil, errors.New("query auth requires a signed query string")
	}

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		cancel()
		return nil, errors.New("invalid archive source URL")
	}
	applyCredentials(request, source)
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: redirectPolicy(resolver),
	}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		cause := ErrFetchFailed
		if errors.Is(err, context.Canceled) {
			cause = context.Canceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			cause = context.DeadlineExceeded
		}
		return nil, &fetchFailure{message: "archive request failed for " + redactURL(requestURL), cause: cause}
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		cancel()
		return nil, fmt.Errorf("archive source %s returned HTTP status %d", redactURL(response.Request.URL), response.StatusCode)
	}
	if response.ContentLength > limits.MaxArchiveBytes {
		_ = response.Body.Close()
		cancel()
		return nil, ErrArchiveTooLarge
	}
	return &boundedResponseBody{body: response.Body, limit: limits.MaxArchiveBytes, cancel: cancel}, nil
}

func validateCredentials(source Source) error {
	switch source.AuthType {
	case "", AuthNone, AuthQuery:
		if source.Username != "" || source.Password != "" || source.BearerToken != "" {
			return errors.New("credentials do not match the selected archive auth type")
		}
	case AuthBasic:
		if source.BearerToken != "" || source.Username == "" || strings.ContainsAny(source.Username, ":\r\n") || strings.ContainsAny(source.Password, "\r\n") {
			return errors.New("invalid Basic credentials")
		}
	case AuthBearer:
		if !validBearerToken(source.BearerToken) || source.Username != "" || source.Password != "" {
			return errors.New("invalid bearer credentials")
		}
	default:
		return errors.New("unsupported archive auth type")
	}
	return nil
}

func validBearerToken(token string) bool {
	if token == "" {
		return false
	}
	padding := false
	seenData := false
	for _, ch := range token {
		if ch == '=' {
			if !seenData {
				return false
			}
			padding = true
			continue
		}
		if padding || !isBearerChar(ch) {
			return false
		}
		seenData = true
	}
	return true
}

func isBearerChar(ch rune) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~+/", ch)
}

func applyCredentials(request *http.Request, source Source) {
	switch source.AuthType {
	case AuthBasic:
		request.SetBasicAuth(source.Username, source.Password)
	case AuthBearer:
		request.Header.Set("Authorization", "Bearer "+source.BearerToken)
	}
}

func parseExternalURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return nil, errors.New("invalid archive source URL")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Opaque != "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("archive source must be an external HTTPS URL without userinfo or fragment")
	}
	u.Scheme = "https"
	host := u.Hostname()
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(host, ".") {
		return nil, errors.New("archive source has an invalid host")
	}
	if port := u.Port(); port != "" {
		value, convErr := strconv.Atoi(port)
		if convErr != nil || value < 1 || value > 65535 {
			return nil, errors.New("archive source has an invalid port")
		}
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if ip.Zone() != "" {
			return nil, errors.New("scoped IP addresses are not allowed")
		}
		return u, nil
	}
	if looksLikeNonCanonicalIPv4(host) || !validDNSName(host) {
		return nil, errors.New("archive source has an invalid host")
	}
	return u, nil
}

func validDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !isASCIIAlphaNumeric(ch) && ch != '-' {
				return false
			}
		}
	}
	return true
}

func isASCIIAlphaNumeric(ch rune) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}

func looksLikeNonCanonicalIPv4(host string) bool {
	parts := strings.Split(host, ".")
	allNumeric := true
	for _, part := range parts {
		if part == "" {
			return false
		}
		base := 10
		value := part
		if len(value) > 2 && strings.EqualFold(value[:2], "0x") {
			base, value = 16, value[2:]
		} else if len(value) > 2 && strings.EqualFold(value[:2], "0o") {
			base, value = 8, value[2:]
		}
		if value == "" {
			return false
		}
		for _, ch := range value {
			valid := ch >= '0' && ch <= '9'
			switch base {
			case 16:
				valid = valid || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F'
			case 8:
				valid = ch >= '0' && ch <= '7'
			}
			if !valid {
				allNumeric = false
				break
			}
		}
		if !allNumeric {
			return false
		}
	}
	return allNumeric
}

func validateExternalHost(ctx context.Context, host string, resolver ipResolver) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isExternalIP(ip) {
			return errors.New("archive source resolves to a prohibited IP address")
		}
		return nil
	}
	if strings.EqualFold(host, "localhost") || hasLocalSuffix(host) {
		return errors.New("archive source uses a local hostname")
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return errors.New("archive source DNS lookup failed")
	}
	if len(addresses) == 0 {
		return errors.New("archive source has no DNS addresses")
	}
	for _, address := range addresses {
		if !isExternalIP(address) {
			return errors.New("archive source resolves to a prohibited IP address")
		}
	}
	return nil
}

func hasLocalSuffix(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range []string{"localhost", "local", "localdomain", "internal", "home.arpa", "test", "invalid", "example", "onion"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// Prefix exclusions are intentionally conservative and follow the IANA IPv4
// and IPv6 special-purpose registries; see the deployment context for sources.
var (
	ipv6GlobalUnicast        = netip.MustParsePrefix("2000::/3")
	nat64WellKnown           = netip.MustParsePrefix("64:ff9b::/96")
	globallyReachableSpecial = mustPrefixes(
		"192.0.0.9/32", "192.0.0.10/32",
		"2001:1::1/128", "2001:1::2/128", "2001:1::3/128", "2001:3::/32",
		"2001:4:112::/48", "2001:20::/28", "2001:30::/28",
	)
	prohibitedPrefixes = mustPrefixes(
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20",
	)
)

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

func isExternalIP(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if address.Is6() && nat64WellKnown.Contains(address) {
		bytes := address.As16()
		v4 := netip.AddrFrom4([4]byte(bytes[12:]))
		return isExternalIP(v4)
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if address.Is6() && !ipv6GlobalUnicast.Contains(address) {
		return false
	}
	for _, prefix := range globallyReachableSpecial {
		if prefix.Contains(address) {
			return true
		}
	}
	for _, prefix := range prohibitedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func newFetchTransport(resolver ipResolver, dialOverride dialFunc) *http.Transport {
	return newFetchTransportWithRoots(resolver, dialOverride, nil)
}

func newFetchTransportWithRoots(resolver ipResolver, dialOverride dialFunc, roots *x509.CertPool) *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  20 * time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid connection address")
			}
			ips, err := resolveExternalIPs(ctx, host, resolver)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				remote := net.JoinHostPort(ip.String(), port)
				if dialOverride != nil {
					conn, dialErr := dialOverride(ctx, network, remote)
					if dialErr == nil {
						return conn, nil
					}
					lastErr = dialErr
					continue
				}
				conn, dialErr := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, remote)
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("archive source has no connectable external IP")
		},
	}
}

func resolveExternalIPs(ctx context.Context, host string, resolver ipResolver) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		if !isExternalIP(address) {
			return nil, errors.New("archive source resolves to a prohibited IP address")
		}
		return []netip.Addr{address.Unmap()}, nil
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("archive source DNS lookup failed")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if !isExternalIP(address) {
			return nil, errors.New("archive source resolves to a prohibited IP address")
		}
		result = append(result, address.Unmap())
	}
	return result, nil
}

func sameAuthority(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func redirectPolicy(resolver ipResolver) func(*http.Request, []*http.Request) error {
	return func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if _, err := parseExternalURL(next.URL.String()); err != nil {
			return err
		}
		if err := validateExternalHost(next.Context(), next.URL.Hostname(), resolver); err != nil {
			return err
		}
		if !sameAuthority(via[len(via)-1].URL, next.URL) {
			next.Header.Del("Authorization")
			next.Header.Del("Cookie")
		}
		return nil
	}
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return "443"
}

func redactURL(u *url.URL) string {
	if u == nil {
		return "<invalid-url>"
	}
	return u.Scheme + "://" + u.Host + "/<redacted>"
}

type boundedResponseBody struct {
	body   io.ReadCloser
	limit  int64
	read   int64
	cancel context.CancelFunc
}

func (b *boundedResponseBody) Read(p []byte) (int, error) {
	remaining := b.limit - b.read
	if remaining < 0 {
		return 0, ErrArchiveTooLarge
	}
	maxRead := remaining + 1
	if int64(len(p)) > maxRead {
		p = p[:maxRead]
	}
	n, err := b.body.Read(p)
	b.read += int64(n)
	if b.read > b.limit {
		return n, ErrArchiveTooLarge
	}
	return n, err
}

func (b *boundedResponseBody) Close() error {
	err := b.body.Close()
	b.cancel()
	return err
}
