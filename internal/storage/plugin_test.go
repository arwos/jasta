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

func TestMigrateAddsProgressEventStorageToExistingAdminDatabase(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "old-admin.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	legacy := migrations()[0]
	legacy.Data = map[string]string{"0001_initial.sql": initialMigration}
	if err := orm.NewMigrate(database, orm.NewVirtualFS([]orm.Migration{legacy})).Run(ctx); err != nil {
		t.Fatalf("run old schema migration: %v", err)
	}
	if err := database.Tag("master").CallContext(ctx, "seed pre-progress job", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO jobs (id, kind, status, stage, progress, site_id, error, created_at)
			VALUES ('old-job', 'deploy', 'failed', 'build', 25, NULL, 'build_failed', 10)`)
		return err
	}); err != nil {
		t.Fatalf("seed old job: %v", err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("upgrade old admin database: %v", err)
	}
	if err := database.Tag("master").CallContext(ctx, "verify progress migration", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "PRAGMA table_info(jobs)")
		if err != nil {
			return err
		}
		ownerSessionColumn := false
		for rows.Next() {
			var cid int
			var name, dataType string
			var notNull, primaryKey int
			var defaultValue any
			if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				_ = rows.Close()
				return err
			}
			if name == "owner_session_id" {
				ownerSessionColumn = true
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if !ownerSessionColumn {
			return fmt.Errorf("owner_session_id column was not migrated")
		}
		rows, err = db.QueryContext(ctx, "SELECT id, status FROM jobs WHERE id = 'old-job'")
		if err != nil {
			return err
		}
		if !rows.Next() {
			_ = rows.Close()
			return fmt.Errorf("pre-existing job was not preserved")
		}
		var id, status string
		if err = rows.Scan(&id, &status); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if id != "old-job" || status != "failed" {
			return fmt.Errorf("pre-existing job changed: %s %s", id, status)
		}
		rows, err = db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='job_events'")
		if err != nil {
			return err
		}
		if !rows.Next() {
			_ = rows.Close()
			return fmt.Errorf("job_events table was not created")
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	}); err != nil {
		t.Fatalf("verify progress migration: %v", err)
	}
}
