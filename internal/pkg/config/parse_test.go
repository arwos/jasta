package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchApplicationConfigsResolvesMarkdownSourceAndOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "docs.yaml")
	content := `type: mda
source: ./content
domains:
  - docs.example
root: release
assets:
  path: /assets
pages:
  index: index.html
  not_found: 404.html
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	configs := SearchApplicationConfigs(dir)
	if len(configs) != 1 {
		t.Fatalf("SearchApplicationConfigs() returned %d configs, want 1", len(configs))
	}
	markdown, ok := configs[0].(*MarkdownApplication)
	if !ok {
		t.Fatalf("config type = %T, want *MarkdownApplication", configs[0])
	}
	if want := filepath.Join(dir, "content"); markdown.Source != want {
		t.Errorf("Markdown source = %q, want %q", markdown.Source, want)
	}
	if want := filepath.Join(dir, "release"); markdown.Root != want {
		t.Errorf("Markdown root = %q, want %q", markdown.Root, want)
	}
}
