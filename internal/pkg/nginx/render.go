package nginx

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.arwos.org/jasta/internal/pkg/host"
)

func Render(sites []Site, options Options) ([]byte, error) {
	settings := Settings{
		AdminDomain: options.AdminDomain, PublicProxy: options.PublicProxy, AdminProxy: options.AdminProxy,
		ChallengeRoot: options.ChallengeRoot, AdminCertificate: options.AdminCertificate, AdminPrivateKey: options.AdminPrivateKey,
	}
	settings.SetDefaults()
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	options = settings.RenderOptions()
	adminDomain, err := normalizeDomain(options.AdminDomain)
	if err != nil {
		return nil, fmt.Errorf("normalize admin domain: %w", err)
	}
	var out strings.Builder
	allDomains := make(map[string]int)
	for i, site := range sites {
		domains := make([]string, 0, len(site.Domains))
		seen := make(map[string]struct{}, len(site.Domains))
		for _, domain := range site.Domains {
			if strings.Contains(domain, ":") || strings.TrimSpace(domain) != domain {
				return nil, fmt.Errorf("site %d has a domain with an unsupported port or whitespace", i)
			}
			normalized, normalizeErr := normalizeDomain(domain)
			if normalizeErr != nil {
				return nil, fmt.Errorf("site %d has an invalid domain: %w", i, normalizeErr)
			}
			if normalized == adminDomain {
				return nil, errors.New("admin domain conflicts with a public site domain")
			}
			if _, exists := seen[normalized]; exists {
				return nil, fmt.Errorf("site %d repeats domain %q", i, normalized)
			}
			if owner, exists := allDomains[normalized]; exists {
				return nil, fmt.Errorf("domain %q is assigned to sites %d and %d", normalized, owner, i)
			}
			seen[normalized] = struct{}{}
			allDomains[normalized] = i
			domains = append(domains, normalized)
		}
		if len(domains) == 0 {
			return nil, fmt.Errorf("site %d has no domains", i)
		}
		sort.Strings(domains)
		if len(out.String()) > 0 {
			out.WriteByte('\n')
		}
		writePublicServer(&out, domains, options)
	}
	writeAdminServer(&out, adminDomain, options)
	return []byte(out.String()), nil
}

func normalizeDomain(value string) (string, error) {
	if strings.Contains(value, ":") || strings.TrimSpace(value) != value {
		return "", errors.New("domain must not contain a port or whitespace")
	}
	return host.Normalize(value)
}

func writePublicServer(out *strings.Builder, domains []string, options Options) {
	fmt.Fprintf(out, "server {\n\tlisten 80;\n\tserver_name %s;\n\n", strings.Join(domains, " "))
	writeChallengeLocation(out, options.ChallengeRoot)
	writeProxyLocation(out, options.PublicProxy, false)
	out.WriteString("}\n")
}

func writeAdminServer(out *strings.Builder, domain string, options Options) {
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
	fmt.Fprintf(out, "server {\n\tlisten 80;\n\tserver_name %s;\n\n", domain)
	writeChallengeLocation(out, options.ChallengeRoot)
	out.WriteString("\tlocation / {\n\t\treturn 308 https://$host$request_uri;\n\t}\n")
	out.WriteString("}\n")
	if options.AdminCertificate == "" {
		return
	}
	out.WriteByte('\n')
	fmt.Fprintf(out, "server {\n\tlisten 443 ssl;\n\tserver_name %s;\n", domain)
	fmt.Fprintf(out, "\tssl_certificate %s;\n\tssl_certificate_key %s;\n\n", nginxQuote(options.AdminCertificate), nginxQuote(options.AdminPrivateKey))
	writeProxyLocation(out, options.AdminProxy, true)
	out.WriteString("}\n")
}

func writeChallengeLocation(out *strings.Builder, root string) {
	fmt.Fprintf(out, "\tlocation ^~ /.well-known/acme-challenge/ {\n\t\troot %s;\n\t	default_type text/plain;\n\t	try_files $uri =404;\n\t}\n\n", nginxQuote(root))
}

func writeProxyLocation(out *strings.Builder, target string, websocket bool) {
	if websocket {
		fmt.Fprintf(out, "\tlocation = /ws {\n\t\tproxy_pass http://%s;\n\t\tproxy_http_version 1.1;\n\t\tproxy_set_header Upgrade $http_upgrade;\n\t\tproxy_set_header Connection \"upgrade\";\n\t\tproxy_set_header Host $host;\n\t\tproxy_set_header X-Real-IP $remote_addr;\n\t\tproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n\t\tproxy_set_header X-Forwarded-Proto $scheme;\n\t}\n\n", target)
	}
	fmt.Fprintf(out, "\tlocation / {\n\t\tproxy_pass http://%s;\n\t\tproxy_http_version 1.1;\n\t\tproxy_set_header Host $host;\n\t\tproxy_set_header X-Real-IP $remote_addr;\n\t\tproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n\t\tproxy_set_header X-Forwarded-Proto $scheme;\n\t}\n", target)
}

func nginxQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
