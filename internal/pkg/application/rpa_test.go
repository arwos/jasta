package application

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/config"
)

func TestRedirectHandler(t *testing.T) {
	service, err := Initialize(&config.RedirectPageApplication{Redirects: []config.Redirect{
		{Domain: "Old.Example.", UrlFrom: "/", UrlTo: "https://new.example/base", StatusCode: http.StatusMovedPermanently},
		{Domain: "old.example", UrlFrom: "/legacy", UrlTo: "https://archive.example/v2", StatusCode: http.StatusPermanentRedirect},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	for _, test := range []struct {
		host     string
		path     string
		status   int
		location string
	}{
		{host: "OLD.EXAMPLE.:8080", path: "/items/42?lang=en&sort=asc", status: http.StatusMovedPermanently, location: "https://new.example/base/items/42?lang=en&sort=asc"},
		{host: "old.example", path: "/legacy/posts/42?tag=go", status: http.StatusPermanentRedirect, location: "https://archive.example/v2/posts/42?tag=go"},
		{host: "old.example", path: "/legacyish", status: http.StatusMovedPermanently, location: "https://new.example/base/legacyish"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Host = test.host
		response := httptest.NewRecorder()
		service.Handler(web.NewCtx(response, request))
		if response.Code != test.status {
			t.Errorf("GET %s status = %d, want %d", test.path, response.Code, test.status)
		}
		if location := response.Header().Get("Location"); location != test.location {
			t.Errorf("GET %s Location = %q, want %q", test.path, location, test.location)
		}
	}
}

func TestRedirectValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		redirects []config.Redirect
		wantError string
	}{
		{
			name:      "unsupported status",
			redirects: []config.Redirect{{Domain: "old.example", UrlFrom: "/", UrlTo: "https://new.example", StatusCode: http.StatusFound}},
			wantError: "status_code",
		},
		{
			name:      "relative target",
			redirects: []config.Redirect{{Domain: "old.example", UrlFrom: "/", UrlTo: "//new.example", StatusCode: http.StatusMovedPermanently}},
			wantError: "absolute HTTP or HTTPS URL",
		},
		{
			name: "domain cycle",
			redirects: []config.Redirect{
				{Domain: "old.example", UrlFrom: "/", UrlTo: "https://next.example", StatusCode: http.StatusMovedPermanently},
				{Domain: "next.example", UrlFrom: "/", UrlTo: "https://old.example", StatusCode: http.StatusPermanentRedirect},
			},
			wantError: "redirect cycle",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := (&config.RedirectPageApplication{Redirects: test.redirects}).Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, test.wantError)
			}
		})
	}
}
