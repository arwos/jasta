package public

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/snapshot"
)

func TestApplySnapshotAcceptsOnlyIdenticalRevisionRetries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	encode := func(domain string) json.RawMessage {
		t.Helper()
		value, err := json.Marshal(config.SinglePageApplication{
			Domains: []string{domain}, Root: root, Assets: config.Assets{Path: "assets"},
			Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
		})
		if err != nil {
			t.Fatalf("marshal site config: %v", err)
		}
		return value
	}
	initial := snapshot.Snapshot{Version: snapshot.ProtocolVersion, Revision: 7, Sites: []snapshot.Site{{Type: config.SPA, Config: encode("docs.example.test")}}}
	runtime, err := snapshot.Prepare(initial)
	if err != nil {
		t.Fatalf("snapshot.Prepare(): %v", err)
	}
	defer runtime.Close()
	api := &Api{state: runtime}
	if err = api.applySnapshot(initial); err != nil {
		t.Fatalf("apply identical revision retry: %v", err)
	}
	if api.state != runtime || api.state.Revision != 7 {
		t.Fatal("identical retry replaced the active runtime")
	}
	conflict := snapshot.Snapshot{Version: snapshot.ProtocolVersion, Revision: 7, Sites: []snapshot.Site{{Type: config.SPA, Config: encode("other.example.test")}}}
	if err = api.applySnapshot(conflict); err == nil {
		t.Fatal("same revision with different configuration was accepted")
	}
	if api.state != runtime || api.state.Revision != 7 {
		t.Fatal("conflicting revision changed the active runtime")
	}
}
