package application

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/pkg/host"
)

type RedirectEdge struct {
	SourceDomain string
	TargetDomain string
}

type redirectRule struct {
	sourceDomain string
	fromPath     string
	target       url.URL
	targetDomain string
	statusCode   int
}

type rpa struct {
	conf    *config.RedirectPageApplication
	rules   []redirectRule
	domains []string
}

func (a *rpa) setup() error {
	if err := a.conf.Validate(); err != nil {
		return err
	}
	a.rules = make([]redirectRule, 0, len(a.conf.Redirects))
	domains := make(map[string]struct{}, len(a.conf.Redirects))
	for i, rule := range a.conf.Redirects {
		source, err := host.Normalize(rule.Domain)
		if err != nil {
			return fmt.Errorf("redirect %d source domain: %w", i, err)
		}
		fromPath, err := config.NormalizeRedirectPath(rule.UrlFrom)
		if err != nil {
			return fmt.Errorf("redirect %d source path: %w", i, err)
		}
		target, err := url.Parse(rule.UrlTo)
		if err != nil {
			return fmt.Errorf("redirect %d target URL: %w", i, err)
		}
		targetDomain, err := host.Normalize(target.Host)
		if err != nil {
			return fmt.Errorf("redirect %d target domain: %w", i, err)
		}
		targetHost := targetDomain
		if port := target.Port(); port != "" {
			targetHost = net.JoinHostPort(targetDomain, port)
		} else if strings.Contains(targetDomain, ":") {
			targetHost = "[" + targetDomain + "]"
		}
		target.Scheme = strings.ToLower(target.Scheme)
		target.Host = targetHost
		a.rules = append(a.rules, redirectRule{
			sourceDomain: source,
			fromPath:     fromPath,
			target:       *target,
			targetDomain: targetDomain,
			statusCode:   rule.StatusCode,
		})
		domains[source] = struct{}{}
	}

	for domain := range domains {
		a.domains = append(a.domains, domain)
	}
	sort.Strings(a.domains)
	sort.Slice(a.rules, func(i, j int) bool {
		if a.rules[i].sourceDomain != a.rules[j].sourceDomain {
			return a.rules[i].sourceDomain < a.rules[j].sourceDomain
		}
		return len(a.rules[i].fromPath) > len(a.rules[j].fromPath)
	})
	return nil
}

func (a *rpa) Close() {}

func (a *rpa) Domains() []string {
	return a.domains
}

func (a *rpa) RedirectEdges() []RedirectEdge {
	edges := make([]RedirectEdge, 0, len(a.rules))
	for _, rule := range a.rules {
		edges = append(edges, RedirectEdge{SourceDomain: rule.sourceDomain, TargetDomain: rule.targetDomain})
	}
	return edges
}

func (a *rpa) Handler(ctx web.Ctx) {
	requestPath := ctx.URL().Path
	if requestPath == "" {
		requestPath = "/"
	}
	for _, rule := range a.rules {
		if rule.sourceDomain != normalizedRequestHost(ctx.URL().Host) || !redirectPathMatches(rule.fromPath, requestPath) {
			continue
		}

		target := rule.target
		target.Path = redirectTargetPath(target.Path, rule.fromPath, requestPath)
		target.RawPath = ""
		target.RawQuery = ctx.URL().RawQuery
		target.ForceQuery = ctx.URL().ForceQuery
		ctx.Response().Header().Set("Location", target.String())
		ctx.Response().WriteHeader(rule.statusCode)
		return
	}
	ctx.Response().WriteHeader(http.StatusNotFound)
}

func normalizedRequestHost(value string) string {
	hostname, err := host.Normalize(value)
	if err != nil {
		return ""
	}
	return hostname
}

func redirectPathMatches(prefix, requestPath string) bool {
	if prefix == "/" {
		return strings.HasPrefix(requestPath, "/")
	}
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func redirectTargetPath(targetBase, fromPath, requestPath string) string {
	suffix := requestPath
	if fromPath != "/" {
		suffix = strings.TrimPrefix(requestPath, fromPath)
	}
	base := strings.TrimSuffix(targetBase, "/")
	if base == "" {
		base = "/"
	}
	if suffix == "" {
		return base
	}
	if base == "/" {
		return suffix
	}
	if !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}
	joined := base + suffix
	if strings.HasSuffix(suffix, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
