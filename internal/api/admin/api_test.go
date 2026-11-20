package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/storage"
)

func TestProtectedJSONRPCRequiresActiveOwnerSession(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "admin.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	enrollment, err := owner.BeginEnrollment("Jasta test", "owner")
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
	api := &Api{database: database, auth: manager}
	loginPayload, err := json.Marshal(loginParams{Password: "long-test-owner-password", SecondFactor: code})
	if err != nil {
		t.Fatalf("Marshal login params: %v", err)
	}
	loginResult, err := api.login(ctx, nil, loginPayload)
	if err != nil {
		t.Fatalf("auth.login: %v", err)
	}
	pair := loginResult.(session.Tokens)
	protected := api.protect(api.me)

	unauthorizedCtx := web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil))
	_, err = protected(ctx, unauthorizedCtx, nil)
	unauthorizedErr, ok := err.(*rpcError)
	if !ok || unauthorizedErr.code != -32001 {
		t.Fatalf("protected call without token error=%v, want unauthorized", err)
	}

	request := httptest.NewRequest("POST", "/jsonrpc", nil)
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	result, err := protected(ctx, web.NewCtx(httptest.NewRecorder(), request), nil)
	if err != nil {
		t.Fatalf("protected call with active token: %v", err)
	}
	if got := result.(map[string]string)["subject"]; got != "owner" {
		t.Fatalf("auth.me subject=%q, want owner", got)
	}

	params, _ := json.Marshal(refreshParams{RefreshToken: pair.RefreshToken})
	if _, err = api.logout(ctx, web.NewCtx(httptest.NewRecorder(), request), params); err != nil {
		t.Fatalf("logout(): %v", err)
	}
	_, err = protected(ctx, web.NewCtx(httptest.NewRecorder(), request), nil)
	unauthorizedErr, ok = err.(*rpcError)
	if !ok || unauthorizedErr.code != -32001 {
		t.Fatalf("protected call after logout error=%v, want unauthorized", err)
	}
}

func TestDecodeParamsRejectsUnknownAndTrailingValues(t *testing.T) {
	var params siteIDParams
	for _, raw := range []string{`{"site_id":"site-1","extra":true}`, `{"site_id":"site-1"} {}`} {
		if err := decodeParams(json.RawMessage(raw), &params); err == nil {
			t.Fatalf("decodeParams(%s) succeeded, want error", raw)
		}
	}
	if !emptyParams(json.RawMessage(" { } ")) {
		t.Fatal("emptyParams rejected an empty object with whitespace")
	}
}

func TestRegistrySourceSecretsAreEncryptedAndOnlyReturnedForFetch(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "sources.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	codec, err := aesgcm.New(key)
	if err != nil {
		t.Fatalf("aesgcm.New(): %v", err)
	}
	api := &Api{database: database, secretCodec: codec}
	created, err := api.createSource(ctx, nil, json.RawMessage(`{"name":"private registry","url":"https://registry.example.org/archive.zip?signature=query-secret","auth_type":"basic","username":"deploy","password":"password-secret"}`))
	if err != nil {
		t.Fatalf("sources.create: %v", err)
	}
	createdFields := created.(map[string]string)
	var storedURL string
	var storedQuery, storedCredentials []byte
	err = database.Tag("master").CallContext(ctx, "inspect encrypted source", func(ctx context.Context, db orm.DB) error {
		queryer := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		return queryer.QueryRowContext(ctx, "SELECT url, url_query_ciphertext, credentials_ciphertext FROM registry_sources WHERE id = ?", createdFields["id"]).
			Scan(&storedURL, &storedQuery, &storedCredentials)
	})
	if err != nil {
		t.Fatalf("read stored source: %v", err)
	}
	for _, forbidden := range []string{"query-secret", "password-secret", "deploy"} {
		if storedURL == "" || string(storedQuery) == forbidden || string(storedCredentials) == forbidden ||
			containsString(string(storedQuery), forbidden) || containsString(string(storedCredentials), forbidden) {
			t.Fatalf("stored registry data exposed %q", forbidden)
		}
	}
	if storedURL != "https://registry.example.org/archive.zip" {
		t.Fatalf("stored URL=%q, want query-free URL", storedURL)
	}
	loaded, err := api.LoadSource(ctx, createdFields["id"])
	if err != nil {
		t.Fatalf("LoadSource(): %v", err)
	}
	if loaded.URL != "https://registry.example.org/archive.zip?signature=query-secret" || loaded.AuthType != archive.AuthBasic ||
		loaded.Username != "deploy" || loaded.Password != "password-secret" {
		t.Fatalf("decrypted source did not round trip: %#v", loaded)
	}
	listResult, err := api.listSources(ctx, nil, nil)
	if err != nil {
		t.Fatalf("sources.list: %v", err)
	}
	encodedList, err := json.Marshal(listResult)
	if err != nil {
		t.Fatalf("marshal source list: %v", err)
	}
	if containsString(string(encodedList), "query-secret") || containsString(string(encodedList), "password-secret") || containsString(string(encodedList), "deploy") {
		t.Fatalf("sources.list exposed secret material: %s", encodedList)
	}
	if _, err = api.updateSource(ctx, nil, json.RawMessage(`{"id":"`+createdFields["id"]+`","name":"updated registry","url":"https://registry.example.org/next.zip","auth_type":"bearer","bearer_token":"token-secret"}`)); err != nil {
		t.Fatalf("sources.update: %v", err)
	}
	updated, err := api.LoadSource(ctx, createdFields["id"])
	if err != nil || updated.BearerToken != "token-secret" || updated.Password != "" {
		t.Fatalf("updated source=%#v err=%v", updated, err)
	}
	if _, err = api.deleteSource(ctx, nil, json.RawMessage(`{"id":"`+createdFields["id"]+`"}`)); err != nil {
		t.Fatalf("sources.delete: %v", err)
	}
	if _, err = api.LoadSource(ctx, createdFields["id"]); err == nil {
		t.Fatal("LoadSource succeeded after delete")
	}
}

func containsString(value, part string) bool {
	return strings.Contains(value, part)
}
