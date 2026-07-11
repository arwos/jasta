package config

import (
	"errors"
	"fmt"
	"time"
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
	ApplicationType ApplicationType `yaml:"type"`
}

type SinglePageApplication struct {
	Domains []string `yaml:"domains"`
	Root    string   `yaml:"root"`
	Assets  Assets   `yaml:"assets"`
	Pages   Pages    `yaml:"pages"`
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
	Domains []string `yaml:"domains"`
	Root    string   `yaml:"root"`
	Assets  Assets   `yaml:"assets"`
	Pages   Pages    `yaml:"pages"`
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
	Source  string   `yaml:"source"`
	Domains []string `yaml:"domains"`
	Root    string   `yaml:"root"`
	Assets  Assets   `yaml:"assets"`
	Pages   Pages    `yaml:"pages"`
}

func (a MarkdownApplication) GetRoot() string {
	return a.Root
}

func (a *MarkdownApplication) SetRoot(root string) {
	a.Root = root
}

func (a MarkdownApplication) Validate() error {
	switch {
	case len(a.Source) == 0:
		return errors.New("source is required")
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
	Redirects []Redirect `yaml:"redirects"`
}

func (a RedirectPageApplication) Validate() error {
	for i, redirect := range a.Redirects {
		switch {
		case len(redirect.Domain) == 0:
			return fmt.Errorf("domain is required for %d", i)
		case len(redirect.UrlFrom) == 0:
			return fmt.Errorf("urlFrom is required for %d", i)
		case len(redirect.UrlTo) == 0:
			return fmt.Errorf("urlTo is required for %d", i)
		}
	}
	return nil
}

type Assets struct {
	Path     string        `yaml:"path"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
}

type Pages struct {
	Index    string `yaml:"index"`
	NotFound string `yaml:"not_found"`
}

type Redirect struct {
	Domain  string `yaml:"domain"`
	UrlFrom string `yaml:"url_from"`
	UrlTo   string `yaml:"url_to"`
}
