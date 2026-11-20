package release

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/storage"
)

func TestHistoryReturnsCurrentPairAndMeasuresAllReleaseFiles(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "admin.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err = storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	root := filepath.Join(t.TempDir(), "releases")
	for _, release := range []struct {
		id       string
		content  string
		config   string
		metadata string
	}{
		{id: "r1", content: "one", config: "config-one", metadata: "meta-one"},
		{id: "r2", content: "two-two", config: "config-two", metadata: "meta-two"},
	} {
		releaseDir := filepath.Join(root, release.id)
		if err := os.MkdirAll(filepath.Join(releaseDir, "content"), 0o750); err != nil {
			t.Fatal(err)
		}
		writeHistoryFile(t, filepath.Join(releaseDir, "content", "index.html"), release.content)
		writeHistoryFile(t, filepath.Join(releaseDir, "config.json"), release.config)
		writeHistoryFile(t, filepath.Join(releaseDir, "metadata.json"), release.metadata)
	}
	stmt := database.Tag("master")
	if err = stmt.CallContext(ctx, "seed history rows", func(ctx context.Context, db orm.DB) error {
		statements := []string{
			"INSERT INTO sites VALUES ('site-1', 'site', 'spa', 1, 1)",
			"INSERT INTO site_config_snapshots VALUES ('c1', 'site-1', 1, '{}', 100)",
			"INSERT INTO site_config_snapshots VALUES ('c2', 'site-1', 2, '{}', 200)",
			"INSERT INTO releases VALUES ('r1', 'site-1', 'c1', 'upload', 'one.zip', '" + filepath.Join(root, "r1", "content") + "', 'ready', 100)",
			"INSERT INTO releases VALUES ('r2', 'site-1', 'c2', 'url', 'two.zip', '" + filepath.Join(root, "r2", "content") + "', 'ready', 200)",
			"INSERT INTO site_activations VALUES ('site-1', 'r2', 'c2', 200)",
		}
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed history rows: %v", err)
	}

	history, err := History(ctx, stmt, "site-1", root)
	if err != nil {
		t.Fatalf("History(): %v", err)
	}
	if history.CurrentReleaseID != "r2" || history.CurrentConfigSnapshotID != "c2" || history.CurrentConfigRevision != 2 {
		t.Fatalf("current pair = (%s, %s, %d)", history.CurrentReleaseID, history.CurrentConfigSnapshotID, history.CurrentConfigRevision)
	}
	if len(history.Releases) != 2 || history.Releases[0].ID != "r2" || !history.Releases[0].Active || history.Releases[1].Active {
		t.Fatalf("unexpected release history order/state: %#v", history.Releases)
	}
	wantTotal := int64(len("one") + len("config-one") + len("meta-one") + len("two-two") + len("config-two") + len("meta-two"))
	if history.TotalBytes != wantTotal || history.Releases[0].DiskBytes != int64(len("two-two")+len("config-two")+len("meta-two")) {
		t.Fatalf("disk usage: total=%d, latest=%d; want total=%d", history.TotalBytes, history.Releases[0].DiskBytes, wantTotal)
	}
	if !history.Releases[1].CreatedAt.Equal(time.Unix(100, 0).UTC()) {
		t.Fatalf("release timestamp = %s", history.Releases[1].CreatedAt)
	}
}

func TestHistoryRejectsContentPathOutsideReleaseRoot(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "admin.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err = storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	root := filepath.Join(t.TempDir(), "releases")
	if err = database.Tag("master").CallContext(ctx, "seed invalid history path", func(ctx context.Context, db orm.DB) error {
		statements := []string{
			"INSERT INTO sites VALUES ('site-1', 'site', 'spa', 1, 1)",
			"INSERT INTO site_config_snapshots VALUES ('c1', 'site-1', 1, '{}', 100)",
			"INSERT INTO releases VALUES ('r1', 'site-1', 'c1', 'upload', 'one.zip', '/etc/content', 'ready', 100)",
		}
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed invalid history path: %v", err)
	}
	if _, err = History(ctx, database.Tag("master"), "site-1", root); err == nil {
		t.Fatal("History() accepted content path outside releases root")
	}
}

func writeHistoryFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o640); err != nil {
		t.Fatal(err)
	}
}
