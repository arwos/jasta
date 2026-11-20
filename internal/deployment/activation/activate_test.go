package activation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/snapshot"
	"go.arwos.org/jasta/internal/storage"
)

func TestActivateChangesPairAndIncludesOtherActiveSites(t *testing.T) {
	stmt := activationDatabase(t)
	roots := seedActivationData(t, stmt)
	var received snapshot.Snapshot
	revision, err := Activate(context.Background(), stmt, "site-1", "release-2", func(_ context.Context, next snapshot.Snapshot) error {
		received = next
		return nil
	})
	if err != nil {
		t.Fatalf("Activate(): %v", err)
	}
	if revision != 1 || received.Revision != 1 || len(received.Sites) != 2 {
		t.Fatalf("revision=%d snapshot=%#v, want revision 1 with 2 sites", revision, received)
	}
	if received.Sites[0].Type != config.SPA || string(received.Sites[0].Config) != spaConfig(roots["release-2"], "one.test") {
		t.Fatalf("first active site is not the new complete pair: %#v", received.Sites[0])
	}
	assertActivation(t, stmt, "site-1", "release-2", "config-2")
	assertActivation(t, stmt, "site-2", "release-3", "config-3")
	if value := setting(t, stmt, revisionSetting); value != "1" {
		t.Fatalf("active snapshot revision = %q, want 1", value)
	}
}

func TestActivateRollsBackWhenWebRejectsSnapshot(t *testing.T) {
	stmt := activationDatabase(t)
	roots := seedActivationData(t, stmt)
	reloadErr := errors.New("web rejected candidate")
	called := false
	_, err := Activate(context.Background(), stmt, "site-1", "release-2", func(_ context.Context, next snapshot.Snapshot) error {
		called = true
		if len(next.Sites) != 2 || string(next.Sites[0].Config) != spaConfig(roots["release-2"], "one.test") {
			t.Fatalf("reload received incomplete candidate: %#v", next)
		}
		return reloadErr
	})
	if !called || !errors.Is(err, reloadErr) {
		t.Fatalf("Activate() error = %v, callback called=%v", err, called)
	}
	assertActivation(t, stmt, "site-1", "release-1", "config-1")
	assertActivation(t, stmt, "site-2", "release-3", "config-3")
	if value := setting(t, stmt, revisionSetting); value != "" {
		t.Fatalf("failed reload persisted snapshot revision %q", value)
	}
}

func TestActivateRejectsReleaseFromAnotherSiteBeforeReload(t *testing.T) {
	stmt := activationDatabase(t)
	seedActivationData(t, stmt)
	called := false
	_, err := Activate(context.Background(), stmt, "site-1", "release-3", func(context.Context, snapshot.Snapshot) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("Activate() error=%v, callback called=%v; expected ownership error before reload", err, called)
	}
	assertActivation(t, stmt, "site-1", "release-1", "config-1")
}

func TestRollbackRestoresSelectedConfigPairAndRetainsAllReleaseFiles(t *testing.T) {
	stmt := activationDatabase(t)
	roots := seedActivationData(t, stmt)
	if _, err := Activate(context.Background(), stmt, "site-1", "release-2", func(context.Context, snapshot.Snapshot) error { return nil }); err != nil {
		t.Fatalf("activate newer release: %v", err)
	}
	var received snapshot.Snapshot
	revision, err := Rollback(context.Background(), stmt, "site-1", "release-1", func(_ context.Context, next snapshot.Snapshot) error {
		received = next
		return nil
	})
	if err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
	if revision != 2 || string(received.Sites[0].Config) != spaConfig(roots["release-1"], "one.test") {
		t.Fatalf("rollback snapshot revision/config = %d/%s", revision, received.Sites[0].Config)
	}
	assertActivation(t, stmt, "site-1", "release-1", "config-1")
	assertActivation(t, stmt, "site-2", "release-3", "config-3")
	for _, releaseID := range []string{"release-1", "release-2", "release-3"} {
		if _, err := os.Stat(filepath.Join(roots[releaseID], "index.html")); err != nil {
			t.Fatalf("rollback removed %s content: %v", releaseID, err)
		}
	}
	if value := setting(t, stmt, revisionSetting); value != "2" {
		t.Fatalf("rollback revision = %q, want 2", value)
	}
	failedErr := errors.New("web reload failed")
	if _, err = Rollback(context.Background(), stmt, "site-1", "release-2", func(context.Context, snapshot.Snapshot) error { return failedErr }); !errors.Is(err, failedErr) {
		t.Fatalf("failed Rollback() error = %v, want reload failure", err)
	}
	assertActivation(t, stmt, "site-1", "release-1", "config-1")
	for _, releaseID := range []string{"release-1", "release-2", "release-3"} {
		if _, err := os.Stat(filepath.Join(roots[releaseID], "index.html")); err != nil {
			t.Fatalf("failed rollback removed %s content: %v", releaseID, err)
		}
	}
}

func TestActivateReloadsWebWithoutRestartAndServesWholeNewSnapshot(t *testing.T) {
	stmt := activationDatabase(t)
	roots := seedActivationData(t, stmt)
	if err := stmt.CallContext(context.Background(), "seed active revision", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "INSERT INTO settings (key, value, updated_at) VALUES (?, '1', 1)", revisionSetting)
		return err
	}); err != nil {
		t.Fatalf("seed active revision: %v", err)
	}
	oldRuntime, err := snapshot.Prepare(snapshot.Snapshot{
		Version: snapshot.ProtocolVersion, Revision: 1,
		Sites: []snapshot.Site{
			{Type: config.SPA, Config: json.RawMessage(spaConfig(roots["release-1"], "one.test"))},
			{Type: config.SPA, Config: json.RawMessage(spaConfig(roots["release-3"], "two.test"))},
		},
	})
	if err != nil {
		t.Fatalf("prepare initial web state: %v", err)
	}
	var stateMu sync.RWMutex
	state := oldRuntime
	socket := filepath.Join(t.TempDir(), "web-reload.sock")
	server, err := snapshot.Listen(socket, func(next snapshot.Snapshot) error {
		candidate, prepareErr := snapshot.Prepare(next)
		if prepareErr != nil {
			return prepareErr
		}
		stateMu.Lock()
		if candidate.Revision <= state.Revision {
			stateMu.Unlock()
			candidate.Close()
			return errors.New("stale snapshot revision")
		}
		previous := state
		state = candidate
		stateMu.Unlock()
		previous.Close()
		return nil
	})
	if err != nil {
		oldRuntime.Close()
		if errors.Is(err, syscall.EPERM) {
			t.Skip("environment does not permit Unix socket listeners")
		}
		t.Fatalf("listen reload socket: %v", err)
	}
	defer func() {
		_ = server.Close()
		stateMu.RLock()
		defer stateMu.RUnlock()
		state.Close()
	}()

	beforeOne := requestBody(t, &stateMu, &state, "one.test")
	if beforeOne != "release-1" {
		t.Fatalf("initial visitor content = %q, want release-1", beforeOne)
	}
	reload := func(_ context.Context, next snapshot.Snapshot) error {
		data, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return marshalErr
		}
		got, sendErr := snapshot.Send(socket, data)
		if sendErr == nil && got != next.Revision {
			return errors.New("web acknowledged a different revision")
		}
		return sendErr
	}
	revision, err := Activate(context.Background(), stmt, "site-1", "release-2", reload)
	if err != nil {
		t.Fatalf("Activate(): %v", err)
	}
	if revision != 2 {
		t.Fatalf("activated revision = %d, want 2", revision)
	}
	if got := requestBody(t, &stateMu, &state, "one.test"); got != "release-2" {
		t.Fatalf("visitor content after reload = %q, want release-2", got)
	}
	if got := requestBody(t, &stateMu, &state, "two.test"); got != "release-3" {
		t.Fatalf("other active site content after reload = %q, want release-3", got)
	}

	invalidOtherSite, _ := json.Marshal(config.SinglePageApplication{
		Root: roots["release-3"], Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	if err = stmt.CallContext(context.Background(), "corrupt other active config", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE site_config_snapshots SET config_json = ? WHERE id = 'config-3'", string(invalidOtherSite))
		return err
	}); err != nil {
		t.Fatalf("prepare invalid reload candidate: %v", err)
	}
	if _, err = Activate(context.Background(), stmt, "site-1", "release-1", reload); err == nil {
		t.Fatal("Activate() accepted a snapshot rejected by web")
	}
	assertActivation(t, stmt, "site-1", "release-2", "config-2")
	if value := setting(t, stmt, revisionSetting); value != "2" {
		t.Fatalf("rejected reload changed active snapshot revision to %q", value)
	}
	if got := requestBody(t, &stateMu, &state, "one.test"); got != "release-2" {
		t.Fatalf("visitor content after rejected reload = %q, want release-2", got)
	}
	if err = stmt.CallContext(context.Background(), "restore other active config", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE site_config_snapshots SET config_json = ? WHERE id = 'config-3'", spaConfig(roots["release-3"], "two.test"))
		return err
	}); err != nil {
		t.Fatalf("restore other active config: %v", err)
	}
	rollbackRevision, err := Rollback(context.Background(), stmt, "site-1", "release-1", reload)
	if err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
	if rollbackRevision != 3 {
		t.Fatalf("rollback revision = %d, want 3", rollbackRevision)
	}
	assertActivation(t, stmt, "site-1", "release-1", "config-1")
	if got := requestBody(t, &stateMu, &state, "one.test"); got != "release-1" {
		t.Fatalf("visitor content after rollback = %q, want release-1", got)
	}
}

func requestBody(t *testing.T, mu *sync.RWMutex, runtime **snapshot.Runtime, domain string) string {
	t.Helper()
	mu.RLock()
	defer mu.RUnlock()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "http://"+domain+"/", nil)
	service, ok := (*runtime).Handlers[domain]
	if !ok {
		t.Fatalf("web runtime does not route domain %q", domain)
	}
	service.Handler(web.NewCtx(recorder, request))
	return strings.TrimSpace(recorder.Body.String())
}

func activationDatabase(t *testing.T) orm.Stmt {
	t.Helper()
	database := orm.New(context.Background())
	err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "admin.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}})
	if err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	if err = storage.Migrate(context.Background(), database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	t.Cleanup(database.Close)
	return database.Tag("master")
}

func seedActivationData(t *testing.T, stmt orm.Stmt) map[string]string {
	t.Helper()
	roots := make(map[string]string, 3)
	for _, releaseID := range []string{"release-1", "release-2", "release-3"} {
		root := filepath.Join(t.TempDir(), releaseID, "content")
		if err := os.MkdirAll(root, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(releaseID), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "404.html"), []byte("not found"), 0o640); err != nil {
			t.Fatal(err)
		}
		roots[releaseID] = root
	}
	err := stmt.CallContext(context.Background(), "seed activations", func(ctx context.Context, db orm.DB) error {
		statements := []string{
			"INSERT INTO sites VALUES ('site-1', 'one', 'spa', 1, 1)",
			"INSERT INTO sites VALUES ('site-2', 'two', 'spa', 1, 1)",
			"INSERT INTO site_domains VALUES ('one.test', 'site-1', 1)",
			"INSERT INTO site_domains VALUES ('two.test', 'site-2', 1)",
			"INSERT INTO site_config_snapshots VALUES ('config-1', 'site-1', 1, '" + spaConfig(roots["release-1"], "one.test") + "', 1)",
			"INSERT INTO site_config_snapshots VALUES ('config-2', 'site-1', 2, '" + spaConfig(roots["release-2"], "one.test") + "', 2)",
			"INSERT INTO site_config_snapshots VALUES ('config-3', 'site-2', 1, '" + spaConfig(roots["release-3"], "two.test") + "', 1)",
			"INSERT INTO releases VALUES ('release-1', 'site-1', 'config-1', 'upload', 'one.zip', '" + roots["release-1"] + "', 'ready', 1)",
			"INSERT INTO releases VALUES ('release-2', 'site-1', 'config-2', 'upload', 'two.zip', '" + roots["release-2"] + "', 'ready', 2)",
			"INSERT INTO releases VALUES ('release-3', 'site-2', 'config-3', 'upload', 'three.zip', '" + roots["release-3"] + "', 'ready', 1)",
			"INSERT INTO site_activations VALUES ('site-1', 'release-1', 'config-1', 1)",
			"INSERT INTO site_activations VALUES ('site-2', 'release-3', 'config-3', 1)",
		}
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed activation data: %v", err)
	}
	return roots
}

func spaConfig(root, domain string) string {
	encoded, _ := json.Marshal(config.SinglePageApplication{
		Domains: []string{domain}, Root: root, Assets: config.Assets{Path: "assets"},
		Pages: config.Pages{Index: "index.html", NotFound: "404.html"},
	})
	return string(encoded)
}

func assertActivation(t *testing.T, stmt orm.Stmt, siteID, releaseID, configID string) {
	t.Helper()
	var gotRelease, gotConfig string
	err := stmt.CallContext(context.Background(), "read active pair", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT release_id, config_snapshot_id FROM site_activations WHERE site_id = ?", siteID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&gotRelease, &gotConfig)
	})
	if err != nil {
		t.Fatalf("read activation for %s: %v", siteID, err)
	}
	if gotRelease != releaseID || gotConfig != configID {
		t.Fatalf("activation for %s = (%s, %s), want (%s, %s)", siteID, gotRelease, gotConfig, releaseID, configID)
	}
}

func setting(t *testing.T, stmt orm.Stmt, key string) string {
	t.Helper()
	var value string
	err := stmt.CallContext(context.Background(), "read setting", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT value FROM settings WHERE key = ?", key)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err = rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		return rows.Scan(&value)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read setting %q: %v", key, err)
	}
	return value
}
