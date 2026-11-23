package application

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/config"
)

func TestSPAHandler(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "index.html", "spa index")
	writeFixture(t, root, "404.html", "spa not found")
	writeFixture(t, root, "static/readme", "spa file")
	writeFixture(t, root, "assets/app.js", "spa asset")

	service, err := Initialize(&config.SinglePageApplication{
		Domains: []string{"spa.test"},
		Root:    root,
		Assets:  config.Assets{Path: "/assets", CacheTTL: 3600},
		Pages:   config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	for _, test := range []struct {
		path       string
		status     int
		body       string
		cacheValue string
	}{
		{path: "/", status: http.StatusOK, body: "spa index"},
		{path: "/index.html", status: http.StatusOK, body: "spa index"},
		{path: "/static/readme", status: http.StatusOK, body: "spa file"},
		{path: "/client/dashboard", status: http.StatusOK, body: "spa index"},
		{path: "/assets/app.js", status: http.StatusOK, body: "spa asset", cacheValue: "max-age=3600"},
		{path: "/assets/missing.js", status: http.StatusNotFound, body: "spa not found"},
		{path: "/%2e%2e/%2e%2e/secret.txt", status: http.StatusNotFound},
	} {
		response := serveTestRequest(t, service, test.path)
		if response.Code != test.status {
			t.Errorf("GET %s status = %d, want %d", test.path, response.Code, test.status)
		}
		if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
			t.Errorf("GET %s body = %q, want it to contain %q", test.path, response.Body.String(), test.body)
		}
		if test.cacheValue != "" && response.Header().Get("Cache-Control") != test.cacheValue {
			t.Errorf("GET %s Cache-Control = %q, want %q", test.path, response.Header().Get("Cache-Control"), test.cacheValue)
		}
	}
}

func TestMPAHandler(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "index.html", "mpa root")
	writeFixture(t, root, "404.html", "mpa not found")
	writeFixture(t, root, "assets/site.css", "mpa asset")
	writeFixture(t, root, "nested/index.html", "mpa nested")
	writeFixture(t, root, "nested/guide.txt", "mpa file")

	service, err := Initialize(&config.MultiPageApplication{
		Domains: []string{"mpa.test"},
		Root:    root,
		Assets:  config.Assets{Path: "/assets"},
		Pages:   config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{path: "/", status: http.StatusOK, body: "mpa root"},
		{path: "/index.html", status: http.StatusOK, body: "mpa root"},
		{path: "/nested", status: http.StatusOK, body: "mpa nested"},
		{path: "/nested/", status: http.StatusOK, body: "mpa nested"},
		{path: "/nested/guide.txt", status: http.StatusOK, body: "mpa file"},
		{path: "/missing/page", status: http.StatusNotFound, body: "mpa not found"},
		{path: "/assets/missing.css", status: http.StatusNotFound, body: "mpa not found"},
		{path: "/%2e%2e/%2e%2e/secret.txt", status: http.StatusNotFound},
	} {
		response := serveTestRequest(t, service, test.path)
		if response.Code != test.status {
			t.Errorf("GET %s status = %d, want %d", test.path, response.Code, test.status)
		}
		if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
			t.Errorf("GET %s body = %q, want it to contain %q", test.path, response.Body.String(), test.body)
		}
	}
}

func serveTestRequest(t *testing.T, service Service, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "example.test"
	response := httptest.NewRecorder()
	service.Handler(web.NewCtx(response, request))
	return response
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
