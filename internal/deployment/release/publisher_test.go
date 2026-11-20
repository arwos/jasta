package release

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/storage"
)

func TestCreatePersistsImmutableUnactivatedRelease(t *testing.T) {
	dir := t.TempDir()
	db, stmt := testDatabase(t, filepath.Join(dir, "admin.sqlite"))
	seedSite(t, stmt)
	stage := filepath.Join(dir, "archive")
	mustMkdir(t, stage)
	mustWrite(t, filepath.Join(stage, "index.html"), "<h1>published</h1>")
	mustWrite(t, filepath.Join(stage, "404.html"), "not found")
	releases := filepath.Join(dir, "releases")
	t.Cleanup(func() { _ = makeWritable(releases) })
	conf, _ := json.Marshal(config.SinglePageApplication{
		Domains: []string{"example.test"}, Root: ".", Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
	})

	result, err := Create(context.Background(), stmt, Input{
		SiteID: "site-1", AppType: config.SPA, Config: conf, SourceKind: "upload",
		ArchiveName: "site.zip", StagingDir: stage, ReleasesRoot: releases,
		CreatedAt: time.Unix(1_800_000_000, 0),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ID == "" || result.ConfigSnapshotID == "" {
		t.Fatalf("release IDs were not generated: %#v", result)
	}
	if body, err := os.ReadFile(filepath.Join(result.ContentPath, "index.html")); err != nil || string(body) != "<h1>published</h1>" {
		t.Fatalf("read immutable content: body=%q err=%v", body, err)
	}
	if info, err := os.Stat(filepath.Join(result.ContentPath, "index.html")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("content file is writable: mode=%v err=%v", info.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(result.ContentPath), "config.json")); err != nil {
		t.Fatalf("release config snapshot missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(result.ContentPath), "metadata.json")); err != nil {
		t.Fatalf("release metadata missing: %v", err)
	}
	var releasesCount, activationCount int
	query(t, stmt, "SELECT COUNT(*) FROM releases", &releasesCount)
	query(t, stmt, "SELECT COUNT(*) FROM site_activations", &activationCount)
	if releasesCount != 1 || activationCount != 0 {
		t.Fatalf("database counts: releases=%d activations=%d", releasesCount, activationCount)
	}
	if err := db.Tag("master").Close(); err != nil {
		t.Fatalf("close db statement: %v", err)
	}
}

func TestCreateRejectsInvalidBuildWithoutPersistingRelease(t *testing.T) {
	dir := t.TempDir()
	db, stmt := testDatabase(t, filepath.Join(dir, "admin.sqlite"))
	seedSite(t, stmt)
	stage := filepath.Join(dir, "archive")
	mustMkdir(t, stage)
	// The required index is absent, so validation fails before persistence.
	mustWrite(t, filepath.Join(stage, "404.html"), "not found")
	conf, _ := json.Marshal(config.SinglePageApplication{
		Domains: []string{"example.test"}, Root: ".", Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	releases := filepath.Join(dir, "releases")
	t.Cleanup(func() { _ = makeWritable(releases) })
	_, err := Create(context.Background(), stmt, Input{
		SiteID: "site-1", AppType: config.SPA, Config: conf, SourceKind: "upload",
		ArchiveName: "site.zip", StagingDir: stage, ReleasesRoot: releases,
	})
	if err == nil {
		t.Fatal("Create() unexpectedly accepted a missing index page")
	}
	entries, readErr := os.ReadDir(releases)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read release directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed build left release entries: %v", entries)
	}
	var releasesCount, snapshotsCount, activationCount int
	query(t, stmt, "SELECT COUNT(*) FROM releases", &releasesCount)
	query(t, stmt, "SELECT COUNT(*) FROM site_config_snapshots", &snapshotsCount)
	query(t, stmt, "SELECT COUNT(*) FROM site_activations", &activationCount)
	if releasesCount != 0 || snapshotsCount != 0 || activationCount != 0 {
		t.Fatalf("failed build changed DB: releases=%d snapshots=%d activations=%d", releasesCount, snapshotsCount, activationCount)
	}
	if err := db.Tag("master").Close(); err != nil {
		t.Fatalf("close db statement: %v", err)
	}
}

func TestCreateSupportsConfigOnlyRedirectRelease(t *testing.T) {
	dir := t.TempDir()
	_, stmt := testDatabase(t, filepath.Join(dir, "admin.sqlite"))
	seedSiteType(t, stmt, "rpa")
	releases := filepath.Join(dir, "releases")
	t.Cleanup(func() { _ = makeWritable(releases) })
	conf, _ := json.Marshal(config.RedirectPageApplication{Redirects: []config.Redirect{{
		Domain: "example.test", UrlFrom: "/", UrlTo: "https://destination.test", StatusCode: 301,
	}}})
	result, err := Create(context.Background(), stmt, Input{
		SiteID: "site-1", AppType: config.RPA, Config: conf, SourceKind: "config", ReleasesRoot: releases,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var sourceKind string
	if err := stmt.CallContext(context.Background(), "read redirect release kind", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT source_kind FROM releases WHERE id = ?", result.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&sourceKind)
	}); err != nil {
		t.Fatalf("read source kind: %v", err)
	}
	if sourceKind != "config" {
		t.Fatalf("source_kind = %q, want config", sourceKind)
	}
	if _, err := os.Stat(filepath.Join(result.ContentPath, "index.html")); !os.IsNotExist(err) {
		t.Fatalf("config-only redirect unexpectedly contains index.html: %v", err)
	}
}

func TestCreateBuildsMarkdownIntoRelease(t *testing.T) {
	dir := t.TempDir()
	_, stmt := testDatabase(t, filepath.Join(dir, "admin.sqlite"))
	seedSiteType(t, stmt, "mda")
	stage := filepath.Join(dir, "archive")
	mustMkdir(t, stage)
	mustMkdir(t, filepath.Join(stage, "templates"))
	mustWrite(t, filepath.Join(stage, "templates", "page.html"), "<main>{{.Content}}</main>")
	mustWrite(t, filepath.Join(stage, "home.md"), "---\nslug: /\ntitle: Home\n---\n# Welcome\n")
	releases := filepath.Join(dir, "releases")
	t.Cleanup(func() { _ = makeWritable(releases) })
	conf, _ := json.Marshal(config.MarkdownApplication{
		Domains: []string{"example.test"}, Root: ".", Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "index.html"},
	})
	result, err := Create(context.Background(), stmt, Input{
		SiteID: "site-1", AppType: config.MDA, Config: conf, SourceKind: "upload",
		ArchiveName: "docs.tar.gz", StagingDir: stage, ReleasesRoot: releases,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(result.ContentPath, "index.html"))
	if err != nil {
		t.Fatalf("read generated markdown page: %v", err)
	}
	if string(body) != "<main><h1>Welcome</h1></main>" {
		t.Fatalf("generated markdown page = %q", body)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(result.ContentPath), "source")); !os.IsNotExist(err) {
		t.Fatalf("markdown source was retained in runtime release: %v", err)
	}
}

func testDatabase(t *testing.T, path string) (orm.ORM, orm.Stmt) {
	t.Helper()
	ctx := context.Background()
	database := orm.New(ctx)
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: path, Cache: "private", Mode: "rwc", Journal: "WAL", LockingMode: "NORMAL",
		OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	if err = storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	t.Cleanup(database.Close)
	return database, database.Tag("master")
}

func seedSite(t *testing.T, stmt orm.Stmt) {
	seedSiteType(t, stmt, "spa")
}

func seedSiteType(t *testing.T, stmt orm.Stmt, appType string) {
	t.Helper()
	err := stmt.CallContext(context.Background(), "seed test site", func(ctx context.Context, db orm.DB) error {
		if _, err := db.ExecContext(ctx, "INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('site-1', 'site', ?, 1, 1)", appType); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, "INSERT INTO site_domains (domain, site_id, created_at) VALUES ('example.test', 'site-1', 1)")
		return err
	})
	if err != nil {
		t.Fatalf("seed site: %v", err)
	}
}

func query(t *testing.T, stmt orm.Stmt, sqlQuery string, target *int) {
	t.Helper()
	err := stmt.CallContext(context.Background(), "query test database", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, sqlQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(target)
	})
	if err != nil {
		t.Fatalf("query %q: %v", sqlQuery, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}
