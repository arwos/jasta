package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/nginx"
)

func TestServeFrontendIsLimitedToAdminHostAndDoesNotShadowAPIRoutes(t *testing.T) {
	api := &Api{conf: Config{Nginx: nginx.Settings{AdminDomain: "admin.example.test"}}}
	for _, test := range []struct {
		name        string
		host        string
		path        string
		accept      string
		status      int
		bodyHas     string
		contentType string
	}{
		{name: "index", host: "admin.example.test", path: "/", accept: "text/html", status: http.StatusOK, bodyHas: "Jasta", contentType: "text/html"},
		{name: "client side route", host: "admin.example.test", path: "/sites", accept: "text/html", status: http.StatusOK, bodyHas: "Jasta", contentType: "text/html"},
		{name: "API path stays 404", host: "admin.example.test", path: "/jsonrpc", accept: "text/html", status: http.StatusNotFound},
		{name: "wrong host stays 404", host: "public.example.test", path: "/", accept: "text/html", status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Host = test.host
			request.Header.Set("Accept", test.accept)
			response := httptest.NewRecorder()
			api.serveFrontend(web.NewCtx(response, request))
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			if test.bodyHas != "" && !strings.Contains(response.Body.String(), test.bodyHas) {
				t.Fatalf("body does not contain %q", test.bodyHas)
			}
			if test.contentType != "" && !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("Content-Type=%q, want %q", response.Header().Get("Content-Type"), test.contentType)
			}
		})
	}
}
