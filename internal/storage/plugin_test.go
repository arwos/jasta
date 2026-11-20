package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
)

func TestMigrateReleaseSourceKindPreservesRowsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "legacy.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	stmt := database.Tag("master")
	if err = stmt.CallContext(ctx, "create legacy schema", func(ctx context.Context, db orm.DB) error {
		statements := []string{
			"CREATE TABLE sites (id TEXT PRIMARY KEY, app_type TEXT NOT NULL)",
			"CREATE TABLE site_config_snapshots (id TEXT PRIMARY KEY, site_id TEXT NOT NULL REFERENCES sites(id), revision INTEGER NOT NULL, config_json TEXT NOT NULL, created_at INTEGER NOT NULL)",
			"CREATE TABLE releases (id TEXT PRIMARY KEY, site_id TEXT NOT NULL REFERENCES sites(id), config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id), source_kind TEXT NOT NULL CHECK (source_kind IN ('upload', 'url')), archive_name TEXT NOT NULL, content_path TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL)",
			"CREATE INDEX releases_site_created_idx ON releases(site_id, created_at DESC)",
			"CREATE TABLE site_activations (site_id TEXT PRIMARY KEY REFERENCES sites(id), release_id TEXT NOT NULL REFERENCES releases(id), config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id), activated_at INTEGER NOT NULL)",
			"INSERT INTO sites VALUES ('site-1', 'spa')",
			"INSERT INTO site_config_snapshots VALUES ('snapshot-1', 'site-1', 1, '{}', 1)",
			"INSERT INTO releases VALUES ('release-1', 'site-1', 'snapshot-1', 'upload', 'site.zip', '/releases/release-1/content', 'ready', 1)",
			"INSERT INTO site_activations VALUES ('site-1', 'release-1', 'snapshot-1', 1)",
		}
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	if err = migrateReleaseSourceKind(ctx, stmt); err != nil {
		t.Fatalf("migrate legacy release schema: %v", err)
	}
	if err = migrateReleaseSourceKind(ctx, stmt); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err = stmt.CallContext(ctx, "verify migrated schema", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT source_kind FROM releases WHERE id = 'release-1'")
		if err != nil {
			return err
		}
		if !rows.Next() {
			_ = rows.Close()
			return rows.Err()
		}
		var source string
		if err = rows.Scan(&source); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if source != "upload" {
			return fmt.Errorf("source_kind = %q, want upload", source)
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO releases VALUES ('release-2', 'site-1', 'snapshot-1', 'config', '', '/releases/release-2/content', 'ready', 2)"); err != nil {
			return err
		}
		rows, err = db.QueryContext(ctx, "SELECT COUNT(*) FROM site_activations WHERE release_id = 'release-1'")
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		var count int
		if err = rows.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("activation count = %d, want 1", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify migrated schema: %v", err)
	}
}
