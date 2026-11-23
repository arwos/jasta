package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.arwos.org/jasta/internal/pkg/config"
)

func TestPrepareStableDigestForIdenticalSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	configJSON, err := json.Marshal(config.SinglePageApplication{
		Domains: []string{"same.example.test"}, Root: root, Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	value := Snapshot{
		Version: ProtocolVersion, Revision: 4,
		Sites: []Site{{Type: config.SPA, Config: configJSON}},
	}
	first, err := Prepare(value)
	if err != nil {
		t.Fatalf("Prepare(first): %v", err)
	}
	defer first.Close()
	second, err := Prepare(value)
	if err != nil {
		t.Fatalf("Prepare(second): %v", err)
	}
	defer second.Close()
	if first.Digest != second.Digest {
		t.Fatalf("identical snapshots have different digests: %x != %x", first.Digest, second.Digest)
	}
}

func TestPrepareRejectsCrossSiteRedirectCycle(t *testing.T) {
	snapshot := Snapshot{
		Version:  ProtocolVersion,
		Revision: 1,
		Sites: []Site{
			{
				Type:   config.RPA,
				Config: json.RawMessage(`{"redirects":[{"domain":"old.example","url_from":"/","url_to":"https://next.example","status_code":301}]}`),
			},
			{
				Type:   config.RPA,
				Config: json.RawMessage(`{"redirects":[{"domain":"next.example","url_from":"/","url_to":"https://old.example","status_code":308}]}`),
			},
		},
	}
	_, err := Prepare(snapshot)
	if err == nil || !strings.Contains(err.Error(), "redirect cycle") {
		t.Fatalf("Prepare() error = %v, want redirect cycle error", err)
	}
}
