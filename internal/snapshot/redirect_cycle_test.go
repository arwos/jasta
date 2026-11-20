package snapshot

import (
	"encoding/json"
	"strings"
	"testing"

	"go.arwos.org/jasta/internal/pkg/config"
)

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
