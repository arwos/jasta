package application

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v3/pkg/mime"
	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/pkg/urlclean"
)

const (
	wellKnown = "/.well-known"
)

func Initialize(c any) (s Service, _ error) {
	switch sc := c.(type) {
	case *config.SinglePageApplication:
		s = &spa{conf: sc}
	case *config.MultiPageApplication:
		s = &mpa{conf: sc}
	case *config.MarkdownApplication:
		s = &mda{conf: sc}
	case *config.RedirectPageApplication:
		s = &rpa{conf: sc}
	default:
		return nil, fmt.Errorf("unknown application type `%T`", sc)
	}

	return s, s.setup()
}

func cleanRequestPath(value string) (string, error) {
	if strings.ContainsRune(value, '\\') {
		return "", fmt.Errorf("backslash is not allowed in URL path")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}
	return urlclean.Clean(value), nil
}

func cleanContentPath(value string) (string, error) {
	value = strings.TrimPrefix(value, "/")
	return cleanRequestPath(value)
}

func serveFile(ctx web.Ctx, root *os.Root, name string, status int, cache *config.Assets) (bool, error) {
	name, err := cleanContentPath(name)
	if err != nil {
		return false, err
	}
	if name == "" {
		return false, nil
	}
	file, err := root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open content file %q: %w", name, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logx.Error("failed to close content file", "file", name, "err", closeErr)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat content file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}

	ctx.Header().Set("Content-Type", mime.DetectByFilename(name))
	if cache != nil {
		ctx.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", cache.CacheTTL))
	}
	ctx.Response().WriteHeader(status)
	if _, err = io.Copy(ctx.Response(), file); err != nil {
		logx.Error("failed to stream content file", "host", ctx.URL().Host, "file", name, "err", err)
	}
	return true, nil
}

func respondNotFound(ctx web.Ctx, root *os.Root, page string) {
	if page != "" {
		found, err := serveFile(ctx, root, page, http.StatusNotFound, nil)
		if err != nil {
			logx.Error("failed to read not-found page", "file", page, "err", err)
		}
		if found {
			return
		}
	}
	ctx.Response().WriteHeader(http.StatusNotFound)
}

func isAssetPath(requestPath, assetPath string) bool {
	assetPath = strings.Trim(strings.ReplaceAll(assetPath, "\\", "/"), "/")
	if assetPath == "" {
		return false
	}
	return requestPath == assetPath || strings.HasPrefix(requestPath, assetPath+"/")
}

func isWellKnown(requestPath string) bool {
	requestPath = strings.TrimPrefix(requestPath, "/")
	wellKnownPath := strings.TrimPrefix(wellKnown, "/")
	return requestPath == wellKnownPath || strings.HasPrefix(requestPath, wellKnownPath+"/")
}

func isSysFile(page string) bool {
	switch page {
	case "/sitemap.xml",
		"/robots.txt",
		"/manifest.json",
		"/favicon.ico",
		"/browserconfig.xml",
		"/3rdpartylicenses.txt",
		"/BingSiteAuth.xml":
		return true
	default:
		return false
	}
}

func hasFileExtension(pathValue string) bool {
	return filepath.Ext(pathValue) != ""
}

func directoryIndex(requestPath, indexName string) string {
	if requestPath == "" {
		return indexName
	}
	return path.Join(requestPath, indexName)
}
