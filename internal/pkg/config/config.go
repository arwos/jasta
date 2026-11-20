package config

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"go.arwos.org/jasta/internal/pkg/host"
)

type ApplicationType string

const (
	SPA ApplicationType = "spa"
	MPA ApplicationType = "mpa"
	MDA ApplicationType = "mda"
	RPA ApplicationType = "rpa"
)

func GetApplicationConfigByType(t ApplicationType) (any, error) {
	switch t {
	case SPA:
		return &SinglePageApplication{}, nil
	case MPA:
		return &MultiPageApplication{}, nil
	case MDA:
		return &MarkdownApplication{}, nil
	case RPA:
		return &RedirectPageApplication{}, nil
	default:
		return nil, fmt.Errorf("unknown application type [%s]", t)
	}
}

type Base struct {
	ApplicationType ApplicationType `yaml:"type" json:"type"`
}

type SinglePageApplication struct {
	Domains []string `yaml:"domains" json:"domains"`
	Root    string   `yaml:"root" json:"root"`
	Assets  Assets   `yaml:"assets" json:"assets"`
	Pages   Pages    `yaml:"pages" json:"pages"`
}

func (a SinglePageApplication) GetRoot() string {
	return a.Root
}

func (a *SinglePageApplication) SetRoot(root string) {
	a.Root = root
}

func (a SinglePageApplication) Validate() error {
	switch {
	case len(a.Domains) == 0:
		return errors.New("domains is required")
	case len(a.Root) == 0:
		return errors.New("root is required")
	case len(a.Assets.Path) == 0:
		return errors.New("assets path is required")
	case len(a.Pages.Index) == 0:
		return errors.New("pages Index is required")
	case len(a.Pages.NotFound) == 0:
		return errors.New("pages NotFound is required")
	default:
		return nil
	}
}

type MultiPageApplication struct {
	Domains []string `yaml:"domains" json:"domains"`
	Root    string   `yaml:"root" json:"root"`
	Assets  Assets   `yaml:"assets" json:"assets"`
	Pages   Pages    `yaml:"pages" json:"pages"`
}

func (a MultiPageApplication) GetRoot() string {
	return a.Root
}

func (a *MultiPageApplication) SetRoot(root string) {
	a.Root = root
}

func (a MultiPageApplication) Validate() error {
	switch {
	case len(a.Domains) == 0:
		return errors.New("domains is required")
	case len(a.Root) == 0:
		return errors.New("root is required")
	case len(a.Assets.Path) == 0:
		return errors.New("assets path is required")
	case len(a.Pages.Index) == 0:
		return errors.New("pages Index is required")
	case len(a.Pages.NotFound) == 0:
		return errors.New("pages NotFound is required")
	default:
		return nil
	}
}

type MarkdownApplication struct {
	Source  string   `yaml:"source" json:"source"`
	Domains []string `yaml:"domains" json:"domains"`
	Root    string   `yaml:"root" json:"root"`
	Assets  Assets   `yaml:"assets" json:"assets"`
	Pages   Pages    `yaml:"pages" json:"pages"`
}

func (a MarkdownApplication) GetRoot() string {
	return a.Root
}

func (a *MarkdownApplication) SetRoot(root string) {
	a.Root = root
}

func (a MarkdownApplication) Validate() error {
	switch {
	case len(a.Domains) == 0:
		return errors.New("domains is required")
	case len(a.Root) == 0:
		return errors.New("root is required")
	case len(a.Assets.Path) == 0:
		return errors.New("assets path is required")
	case len(a.Pages.Index) == 0:
		return errors.New("pages Index is required")
	case len(a.Pages.NotFound) == 0:
		return errors.New("pages NotFound is required")
	default:
		return nil
	}
}

type RedirectPageApplication struct {
	Redirects []Redirect `yaml:"redirects" json:"redirects"`
}

func (a RedirectPageApplication) Validate() error {
	if len(a.Redirects) == 0 {
		return errors.New("redirects is required")
	}
	graph := make(map[string][]string)
	seen := make(map[string]struct{}, len(a.Redirects))
	for i, redirect := range a.Redirects {
		source, err := host.Normalize(redirect.Domain)
		if err != nil {
			return fmt.Errorf("invalid source domain for redirect %d: %w", i, err)
		}
		from, err := NormalizeRedirectPath(redirect.UrlFrom)
		if err != nil {
			return fmt.Errorf("invalid url_from for redirect %d: %w", i, err)
		}
		key := source + "\x00" + from
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate redirect rule for domain %q and path %q", source, from)
		}
		seen[key] = struct{}{}

		target, err := url.Parse(redirect.UrlTo)
		if err != nil {
			return fmt.Errorf("invalid url_to for redirect %d: %w", i, err)
		}
		scheme := strings.ToLower(target.Scheme)
		if !target.IsAbs() || (scheme != "http" && scheme != "https") || target.Host == "" || target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" {
			return fmt.Errorf("url_to for redirect %d must be an absolute HTTP or HTTPS URL without credentials, query, or fragment", i)
		}
		destination, err := host.Normalize(target.Host)
		if err != nil {
			return fmt.Errorf("invalid target domain for redirect %d: %w", i, err)
		}
		for _, segment := range strings.Split(target.Path, "/") {
			if segment == ".." {
				return fmt.Errorf("url_to path traversal is not allowed for redirect %d", i)
			}
		}
		if redirect.StatusCode != 301 && redirect.StatusCode != 308 {
			return fmt.Errorf("status_code for redirect %d must be 301 or 308", i)
		}
		graph[source] = append(graph[source], destination)
	}
	if err := validateRedirectGraph(graph); err != nil {
		return err
	}
	return nil
}

func NormalizeRedirectPath(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\\') {
		return "", fmt.Errorf("must be an absolute URL path")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("must contain only a URL path")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}
	return path.Clean(parsed.Path), nil
}

func validateRedirectGraph(graph map[string][]string) error {
	const (
		visiting = 1
		visited  = 2
	)
	state := make(map[string]int, len(graph))
	var visit func(string) error
	visit = func(domain string) error {
		if state[domain] == visiting {
			return fmt.Errorf("redirect cycle includes domain %q", domain)
		}
		if state[domain] == visited {
			return nil
		}
		state[domain] = visiting
		for _, target := range graph[domain] {
			if _, redirectSource := graph[target]; redirectSource {
				if err := visit(target); err != nil {
					return err
				}
			}
		}
		state[domain] = visited
		return nil
	}
	for domain := range graph {
		if err := visit(domain); err != nil {
			return err
		}
	}
	return nil
}

type Assets struct {
	Path     string `yaml:"path" json:"path"`
	CacheTTL int64  `yaml:"cache_ttl" json:"cache_ttl"`
}

type Pages struct {
	Index    string `yaml:"index" json:"index"`
	NotFound string `yaml:"not_found" json:"not_found"`
}

type Redirect struct {
	Domain     string `yaml:"domain" json:"domain"`
	UrlFrom    string `yaml:"url_from" json:"url_from"`
	UrlTo      string `yaml:"url_to" json:"url_to"`
	StatusCode int    `yaml:"status_code" json:"status_code"`
}
