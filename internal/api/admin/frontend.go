package admin

import (
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/host"
	adminui "go.arwos.org/jasta/ui/admin"
)

func (a *Api) serveFrontend(ctx web.Ctx) {
	request := ctx.Request()
	if !adminFrontendHost(request.Host, a.conf.Nginx.AdminDomain) {
		ctx.Error(http.StatusNotFound, fs.ErrNotExist)
		return
	}
	requestPath := request.URL.Path
	if reservedAdminPath(requestPath) {
		ctx.Error(http.StatusNotFound, fs.ErrNotExist)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		ctx.Error(http.StatusMethodNotAllowed, fs.ErrNotExist)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	files := adminui.Files()
	content, err := fs.ReadFile(files, name)
	if err != nil && filepath.Ext(name) == "" && strings.Contains(request.Header.Get("Accept"), "text/html") {
		name = "index.html"
		content, err = fs.ReadFile(files, name)
	}
	if err != nil {
		ctx.Error(http.StatusNotFound, fs.ErrNotExist)
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	ctx.Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Header().Set("Referrer-Policy", "same-origin")
	ctx.Header().Set("Content-Type", contentType)
	if name == "index.html" {
		ctx.Header().Set("Cache-Control", "no-cache")
	} else {
		ctx.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if request.Method == http.MethodHead {
		ctx.Response().WriteHeader(http.StatusOK)
		return
	}
	ctx.Bytes(http.StatusOK, content)
}

func adminFrontendHost(requestHost, configuredDomain string) bool {
	expected, err := host.Normalize(configuredDomain)
	if err != nil || expected == "" {
		return false
	}
	if parsedHost, _, splitErr := net.SplitHostPort(requestHost); splitErr == nil {
		requestHost = parsedHost
	}
	actual, err := host.Normalize(requestHost)
	return err == nil && actual == expected
}

func reservedAdminPath(value string) bool {
	clean := path.Clean("/" + value)
	for _, prefix := range []string{"/jsonrpc", "/uploads", "/ws", "/.well-known"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}
