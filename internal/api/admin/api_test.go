package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/auth/session"
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
