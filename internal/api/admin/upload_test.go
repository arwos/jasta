package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/storage"
)

func TestUploadArchiveRequiresActiveSessionAndPublishesOnlyValidRelease(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "upload.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	enrollment, err := owner.BeginEnrollment("Jasta upload test", "owner")
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
	if err = database.Tag("master").CallContext(ctx, "seed upload target site", func(ctx context.Context, db orm.DB) error {
		for _, query := range []string{
			`INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('site-1', 'Test site', 'spa', 1, 1)`,
			`INSERT INTO site_domains (domain, site_id, created_at) VALUES ('upload.example.test', 'site-1', 1)`,
		} {
			if _, err := db.ExecContext(ctx, query); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed upload site: %v", err)
	}
	releasesRoot := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(releasesRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			_ = os.Chmod(path, 0o700)
			return nil
		})
	})
	api := &Api{
		conf:     Config{Archive: archive.Limits{MaxArchiveBytes: 1024, MaxUnpacked: 2048, MaxEntries: 10}, ReleasesRoot: releasesRoot},
		database: database, auth: manager,
	}
	api.jobs, err = jobs.New(database.Tag("master"))
	if err != nil {
		t.Fatalf("jobs.New(): %v", err)
	}
	configJSON := `{"domains":["upload.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`
	validArchive := makeUploadTestZIP(t, map[string]string{
		"index.html": "<h1>uploaded</h1>", "404.html": "not found", "assets/app.js": "console.log('ready')",
	})

	unauthorized := uploadTestRequest(t, "site-1", configJSON, validArchive, tokens.AccessToken)
	unauthorized.Header.Set("Authorization", "Bearer invalid")
	unauthorizedRecorder := httptest.NewRecorder()
	api.uploadArchive(web.NewCtx(unauthorizedRecorder, unauthorized))
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized upload status=%d, want %d", unauthorizedRecorder.Code, http.StatusUnauthorized)
	}
	malformed := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewBufferString("broken multipart body"))
	malformed.Header.Set("Content-Type", "multipart/form-data; boundary=missing-end")
	malformed.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	malformedRecorder := httptest.NewRecorder()
	api.uploadArchive(web.NewCtx(malformedRecorder, malformed))
	if malformedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed multipart status=%d, want %d", malformedRecorder.Code, http.StatusBadRequest)
	}

	invalidRecorder := uploadTestCall(t, api, "site-1", configJSON, []byte("not a zip"), tokens.AccessToken)
	if invalidRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid archive status=%d, want %d body=%s", invalidRecorder.Code, http.StatusUnprocessableEntity, invalidRecorder.Body)
	}
	var invalidResult map[string]string
	if err = json.Unmarshal(invalidRecorder.Body.Bytes(), &invalidResult); err != nil || invalidResult["job_id"] == "" {
		t.Fatalf("invalid archive response=%s err=%v, want failed job ID", invalidRecorder.Body, err)
	}
	var releaseCount int
	if err = database.Tag("master").CallContext(ctx, "count releases after invalid upload", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		return queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM releases").Scan(&releaseCount)
	}); err != nil {
		t.Fatalf("count releases after invalid archive: %v", err)
	}
	if releaseCount != 0 {
		t.Fatalf("invalid upload created %d releases, want 0", releaseCount)
	}
	tooLargeRecorder := uploadTestCall(t, api, "site-1", configJSON, bytes.Repeat([]byte("x"), 1025), tokens.AccessToken)
	if tooLargeRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized archive status=%d, want %d body=%s", tooLargeRecorder.Code, http.StatusRequestEntityTooLarge, tooLargeRecorder.Body)
	}

	successRecorder := uploadTestCall(t, api, "site-1", configJSON, validArchive, tokens.AccessToken)
	if successRecorder.Code != http.StatusCreated {
		t.Fatalf("valid upload status=%d, want %d body=%s", successRecorder.Code, http.StatusCreated, successRecorder.Body)
	}
	var response uploadResponse
	if err = json.Unmarshal(successRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if response.JobID == "" || response.Release.ID == "" || response.Release.SiteID != "site-1" {
		t.Fatalf("upload response=%#v", response)
	}
	if strings.Contains(successRecorder.Body.String(), releasesRoot) || strings.Contains(successRecorder.Body.String(), "content_path") {
		t.Fatalf("upload response exposed local release path: %s", successRecorder.Body)
	}
	var status string
	if err = database.Tag("master").CallContext(ctx, "verify completed upload job", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		return queryer.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", response.JobID).Scan(&status)
	}); err != nil {
		t.Fatalf("load upload job status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("upload job status=%q, want succeeded", status)
	}
	if err = manager.Logout(ctx, database.Tag("master"), tokens.RefreshToken); err != nil {
		t.Fatalf("Logout(): %v", err)
	}
	revokedRecorder := uploadTestCall(t, api, "site-1", configJSON, validArchive, tokens.AccessToken)
	if revokedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session upload status=%d, want %d", revokedRecorder.Code, http.StatusUnauthorized)
	}
}

func uploadTestCall(t *testing.T, api *Api, siteID, configJSON string, archiveData []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := uploadTestRequest(t, siteID, configJSON, archiveData, token)
	recorder := httptest.NewRecorder()
	api.uploadArchive(web.NewCtx(recorder, request))
	return recorder
}

func uploadTestRequest(t *testing.T, siteID, configJSON string, archiveData []byte, token string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"site_id": siteID, "config": configJSON} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField(%s): %v", key, err)
		}
	}
	part, err := writer.CreateFormFile("archive", "site.zip")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err = io.Copy(part, bytes.NewReader(archiveData)); err != nil {
		t.Fatalf("write archive field: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("Close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func makeUploadTestZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err = io.WriteString(file, content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buffer.Bytes()
}
