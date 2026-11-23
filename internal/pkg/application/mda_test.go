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

func TestMarkdownApplicationBuildsAndServesPages(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	output := filepath.Join(base, "release")
	mdaWriteFixture(t, filepath.Join(source, "templates", "page.html"), `<!doctype html><html><head><title>{{.Title}}</title><meta name="description" content="{{.Description}}"></head><body><main>{{.Content}}</main><p>{{.Slug}}</p></body></html>`)
	mdaWriteFixture(t, filepath.Join(source, "templates", "article.html"), `<article data-template="article"><h1>{{.Title}}</h1>{{.Content}}</article>`)
	mdaWriteFixture(t, filepath.Join(source, "index.md"), "---\nslug: /\ntitle: '<Home>'\ndescription: '<summary>'\n---\n# Welcome\n\n<script>alert(1)</script>\n")
	mdaWriteFixture(t, filepath.Join(source, "guides", "start.markdown"), "---\nslug: /guides/start\ntitle: Guide\ntemplate: article.html\n---\nRead **this**.\n")
	mdaWriteFixture(t, filepath.Join(source, "assets", "site.css"), "body { color: red; }\n")
	conf := markdownTestConfig(source, output)
	if err := BuildMarkdownSite(conf); err != nil {
		t.Fatal(err)
	}
	servingConfig := *conf
	servingConfig.Source = ""
	service, err := Initialize(&servingConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	for _, test := range []struct {
		path       string
		status     int
		want       []string
		wantAbsent []string
	}{
		{path: "/", status: http.StatusOK, want: []string{"&lt;Home&gt;", "&lt;summary&gt;", "<h1>Welcome</h1>", "&lt;script&gt;alert(1)&lt;/script&gt;"}, wantAbsent: []string{"<script>"}},
		{path: "/guides/start", status: http.StatusOK, want: []string{`data-template="article"`, "<strong>this</strong>", "Guide"}},
		{path: "/assets/site.css", status: http.StatusOK, want: []string{"body { color: red; }"}},
		{path: "/missing", status: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Host = "docs.example"
		service.Handler(web.NewCtx(recorder, request))
		if recorder.Code != test.status {
			t.Errorf("GET %s status = %d, want %d", test.path, recorder.Code, test.status)
		}
		for _, fragment := range test.want {
			if !strings.Contains(recorder.Body.String(), fragment) {
				t.Errorf("GET %s body %q does not contain %q", test.path, recorder.Body.String(), fragment)
			}
		}
		for _, fragment := range test.wantAbsent {
			if strings.Contains(recorder.Body.String(), fragment) {
				t.Errorf("GET %s body %q unexpectedly contains %q", test.path, recorder.Body.String(), fragment)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(output, "guides", "start", "index.html")); err != nil {
		t.Fatalf("generated page is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "templates", "page.html")); !os.IsNotExist(err) {
		t.Fatalf("template leaked into public output, stat error = %v", err)
	}
}

func TestMarkdownApplicationRejectsInvalidPagesWithoutReplacingOutput(t *testing.T) {
	for _, test := range []struct {
		name     string
		markdown string
		other    string
		wantErr  string
	}{
		{name: "missing front matter", markdown: "# Hello\n", wantErr: "front matter"},
		{name: "invalid YAML", markdown: "---\nslug: [broken\n---\n# Hello\n", wantErr: "decode front matter"},
		{name: "missing slug", markdown: "---\ntitle: Hello\n---\n# Hello\n", wantErr: "slug"},
		{name: "unknown front matter field", markdown: "---\nslug: /\ncustom: value\n---\n# Hello\n", wantErr: "custom"},
		{name: "invalid slug", markdown: "---\nslug: /../secret\n---\n# Hello\n", wantErr: "invalid slug"},
		{name: "duplicate normalized slug", markdown: "---\nslug: /guide/\n---\nOne\n", other: "---\nslug: /guide\n---\nTwo\n", wantErr: "same page"},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source")
			output := filepath.Join(base, "release")
			mdaWriteFixture(t, filepath.Join(source, "templates", "page.html"), `{{.Content}}`)
			mdaWriteFixture(t, filepath.Join(source, "bad.md"), test.markdown)
			if test.other != "" {
				mdaWriteFixture(t, filepath.Join(source, "second.md"), test.other)
			}
			mdaWriteFixture(t, filepath.Join(output, "previous.txt"), "previous release")
			err := BuildMarkdownSite(markdownTestConfig(source, output))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Initialize() error = %v, want it to contain %q", err, test.wantErr)
			}
			previous, readErr := os.ReadFile(filepath.Join(output, "previous.txt"))
			if readErr != nil || string(previous) != "previous release" {
				t.Fatalf("failed build changed previous output: content = %q, error = %v", previous, readErr)
			}
		})
	}
}

func TestMarkdownApplicationKeepsPreviousOutput(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	output := filepath.Join(base, "release")
	mdaWriteFixture(t, filepath.Join(source, "templates", "page.html"), `{{.Content}}`)
	mdaWriteFixture(t, filepath.Join(source, "home.md"), "---\nslug: /\n---\nNew version\n")
	mdaWriteFixture(t, filepath.Join(output, "old.txt"), "previous release")

	if err := BuildMarkdownSite(markdownTestConfig(source, output)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "index.html")); err != nil {
		t.Fatalf("new generated output is missing: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".jasta-markdown-backup-") {
			continue
		}
		previous, readErr := os.ReadFile(filepath.Join(base, entry.Name(), "old.txt"))
		if readErr == nil && string(previous) == "previous release" {
			return
		}
	}
	t.Fatal("previous output was not retained in a backup directory")
}

func TestNormalizeMarkdownSlug(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
		bad   bool
	}{
		{value: "/", want: "/"},
		{value: "/guides/start/", want: "/guides/start"},
		{value: "/café", want: "/café"},
		{value: "/%2e%2e/secret", bad: true},
		{value: "/guide?draft=true", bad: true},
	} {
		got, err := normalizeMarkdownSlug(test.value)
		if (err != nil) != test.bad || got != test.want {
			t.Errorf("normalizeMarkdownSlug(%q) = %q, %v; want %q, error=%v", test.value, got, err, test.want, test.bad)
		}
	}
}

func markdownTestConfig(source, output string) *config.MarkdownApplication {
	return &config.MarkdownApplication{
		Source:  source,
		Domains: []string{"docs.example"},
		Root:    output,
		Assets:  config.Assets{Path: "/assets"},
		Pages:   config.Pages{Index: "index.html", NotFound: "404.html"},
	}
}

func mdaWriteFixture(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
