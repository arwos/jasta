package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/deployment/release"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/storage"
)

func TestCreateReleaseFromRegistryAndRedirectConfig(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "deploy.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	enrollment, err := owner.BeginEnrollment("Jasta deployment test", "owner")
	if err != nil {
		t.Fatalf("BeginEnrollment(): %v", err)
	}
	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode(): %v", err)
	}
	if _, err = owner.Bootstrap(ctx, database.Tag("master"), "long-test-owner-password", enrollment.Secret, code, key); err != nil {
		t.Fatalf("Bootstrap(): %v", err)
	}
	manager, err := session.New(key)
	if err != nil {
		t.Fatalf("session.New(): %v", err)
	}
	code, err = totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() for login: %v", err)
	}
	tokens, err := manager.Login(ctx, database.Tag("master"), "long-test-owner-password", code)
	if err != nil {
		t.Fatalf("Login(): %v", err)
	}
	releasesRoot := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(releasesRoot, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr == nil && info != nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	api := &Api{conf: Config{
		Archive:      archive.Limits{MaxArchiveBytes: 1 << 20, MaxUnpacked: 2 << 20, MaxEntries: 20},
		ReleasesRoot: releasesRoot,
	}, database: database, auth: manager}
	api.secretCodec, err = aesgcm.New(key)
	if err != nil {
		t.Fatalf("aesgcm.New(): %v", err)
	}
	api.jobs, err = jobs.New(database.Tag("master"))
	if err != nil {
		t.Fatalf("jobs.New(): %v", err)
	}
	if err = database.Tag("master").CallContext(ctx, "seed release deployment sites", func(ctx context.Context, db orm.DB) error {
		for _, query := range []string{
			`INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('spa-site', 'SPA site', 'spa', 1, 1)`,
			`INSERT INTO site_domains (domain, site_id, created_at) VALUES ('registry.example.test', 'spa-site', 1)`,
			`INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('redirect-site', 'Redirect site', 'rpa', 1, 1)`,
			`INSERT INTO site_domains (domain, site_id, created_at) VALUES ('redirect.example.test', 'redirect-site', 1)`,
		} {
			if _, execErr := db.ExecContext(ctx, query); execErr != nil {
				return execErr
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed deployment sites: %v", err)
	}
	sourceResult, err := api.createSource(ctx, nil, json.RawMessage(`{"name":"test registry","url":"https://registry.example.test/releases/site.zip","auth_type":"none"}`))
	if err != nil {
		t.Fatalf("createSource(): %v", err)
	}
	sourceJSON, _ := json.Marshal(sourceResult)
	var source struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(sourceJSON, &source); err != nil || source.ID == "" {
		t.Fatalf("decode source ID: source=%s err=%v", sourceJSON, err)
	}
	archiveBytes := makeUploadTestZIP(t, map[string]string{
		"index.html": "hello", "404.html": "not found", "assets/app.js": "export {};",
	})
	api.archiveFetcher = func(_ context.Context, requested archive.Source, options archive.FetchOptions) (io.ReadCloser, error) {
		if requested.URL != "https://registry.example.test/releases/site.zip" || options.Limits.MaxArchiveBytes != 1<<20 {
			return nil, errors.New("unexpected archive source or limits")
		}
		return io.NopCloser(bytes.NewReader(archiveBytes)), nil
	}
	configJSON := json.RawMessage(`{"domains":["registry.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`)
	created := callCreateRelease(t, ctx, api, tokens.AccessToken, createReleaseParams{SiteID: "spa-site", SourceID: source.ID, Config: configJSON})
	if created.Status != "succeeded" || created.Release == nil || created.Release.ID == "" {
		t.Fatalf("registry release response=%#v, want successful release", created)
	}
	history, err := release.History(ctx, database.Tag("master"), "spa-site", releasesRoot)
	if err != nil {
		t.Fatalf("release.History(): %v", err)
	}
	if len(history.Releases) != 1 || history.Releases[0].SourceKind != "url" || history.Releases[0].ArchiveName != "site.zip" || history.Releases[0].Active {
		t.Fatalf("unexpected registry release history: %#v", history)
	}
	claims, err := manager.VerifyAccess(tokens.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess(): %v", err)
	}
	assertReleaseJobStages(t, ctx, api.jobs, claims.SessionID, created.JobID, []string{"queued", "received", "download", "validate", "build", "publish", "publish"})
	redirectConfig := json.RawMessage(`{"redirects":[{"domain":"redirect.example.test","url_from":"/old","url_to":"https://new.example.test","status_code":308}]}`)
	redirectRelease := callCreateRelease(t, ctx, api, tokens.AccessToken, createReleaseParams{SiteID: "redirect-site", Config: redirectConfig})
	if redirectRelease.Status != "succeeded" || redirectRelease.Release == nil {
		t.Fatalf("redirect release response=%#v, want successful config-only release", redirectRelease)
	}
	redirectHistory, err := release.History(ctx, database.Tag("master"), "redirect-site", releasesRoot)
	if err != nil {
		t.Fatalf("release.History() for redirect: %v", err)
	}
	if len(redirectHistory.Releases) != 1 || redirectHistory.Releases[0].SourceKind != "config" || redirectHistory.Releases[0].ArchiveName != "" {
		t.Fatalf("unexpected redirect release history: %#v", redirectHistory)
	}
	assertReleaseJobStages(t, ctx, api.jobs, claims.SessionID, redirectRelease.JobID, []string{"queued", "received", "build", "publish", "publish"})
	api.archiveFetcher = func(context.Context, archive.Source, archive.FetchOptions) (io.ReadCloser, error) {
		return nil, errors.New("private source failure")
	}
	failed := callCreateRelease(t, ctx, api, tokens.AccessToken, createReleaseParams{SiteID: "spa-site", SourceID: source.ID, Config: configJSON})
	if failed.Status != "failed" || failed.ErrorCode != "source_fetch_failed" || failed.Release != nil {
		t.Fatalf("failed fetch response=%#v, want safe public error code", failed)
	}
	assertReleaseJobStages(t, ctx, api.jobs, claims.SessionID, failed.JobID, []string{"queued", "received", "download", "download"})
}

func assertReleaseJobStages(t *testing.T, ctx context.Context, manager *jobs.Manager, ownerSessionID, jobID string, expected []string) {
	t.Helper()
	replay, unsubscribe, err := manager.Subscribe(ctx, ownerSessionID, 0, func(jobs.Event) {})
	if err != nil {
		t.Fatalf("jobs.Subscribe(): %v", err)
	}
	defer unsubscribe()
	actual := make([]string, 0, len(expected))
	for _, event := range replay.Events {
		if event.JobID == jobID {
			actual = append(actual, event.Stage)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("job %s stages=%v, want %v", jobID, actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("job %s stages=%v, want %v", jobID, actual, expected)
		}
	}
}

func callCreateRelease(t *testing.T, ctx context.Context, api *Api, accessToken string, params createReleaseParams) createReleaseResult {
	t.Helper()
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal create release params: %v", err)
	}
	request := httptest.NewRequest("POST", "/jsonrpc", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	value, err := api.JSONRPCApiHandlers()["releases.create"](ctx, web.NewCtx(httptest.NewRecorder(), request), payload)
	if err != nil {
		t.Fatalf("releases.create: %v", err)
	}
	result, ok := value.(createReleaseResult)
	if !ok {
		t.Fatalf("releases.create result type=%T, want createReleaseResult", value)
	}
	return result
}
