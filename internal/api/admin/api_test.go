package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pquerna/otp/totp"
	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/goppy/v3/plugins/ws"

	"go.arwos.org/jasta/internal/auth/owner"
	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/pkg/nginx"
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
	nginxDir := t.TempDir()
	nginxRunner := &adminNginxTestRunner{}
	api := &Api{
		conf: Config{Nginx: nginx.Settings{
			AdminDomain: "admin.example.test", ConfigPath: filepath.Join(nginxDir, "jasta.conf"),
			ChallengeRoot: filepath.Join(nginxDir, "challenges"),
		}, CertificatesRoot: filepath.Join(nginxDir, "certificates")},
		database: database, auth: manager, wsServer: ws.NewServer(ctx), nginxRunner: nginxRunner,
	}
	api.jobs, err = jobs.New(database.Tag("master"))
	if err != nil {
		t.Fatalf("jobs.New(): %v", err)
	}
	t.Cleanup(api.wsServer.CloseAll)
	loginPayload, err := json.Marshal(loginParams{Password: "long-test-owner-password", SecondFactor: code})
	if err != nil {
		t.Fatalf("Marshal login params: %v", err)
	}
	loginResult, err := api.login(ctx, nil, loginPayload)
	if err != nil {
		t.Fatalf("auth.login: %v", err)
	}
	pair := loginResult.(session.Tokens)
	createSite := api.JSONRPCApiHandlers()["sites.create"]
	createPayload := json.RawMessage(`{"name":"Redirect test","app_type":"rpa","config":{"redirects":[{"domain":"Old.Example.Test.","url_from":"/old","url_to":"https://new.example.test","status_code":301},{"domain":"old.example.test","url_from":"/legacy","url_to":"https://new.example.test","status_code":308}]}}`)
	if _, err = createSite(ctx, web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil)), createPayload); err == nil {
		t.Fatal("sites.create accepted a request without owner bearer token")
	}
	createRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	createRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	createdResult, err := createSite(ctx, web.NewCtx(httptest.NewRecorder(), createRequest), createPayload)
	if err != nil {
		t.Fatalf("sites.create: %v", err)
	}
	createdJSON, err := json.Marshal(createdResult)
	if err != nil {
		t.Fatalf("marshal created site: %v", err)
	}
	var created siteSummary
	if err = json.Unmarshal(createdJSON, &created); err != nil {
		t.Fatalf("decode created site: %v", err)
	}
	if created.ID == "" || created.AppType != "rpa" || len(created.Domains) != 1 || created.Domains[0] != "old.example.test" {
		t.Fatalf("unexpected created redirect site: %#v", created)
	}
	for _, testCase := range []struct {
		name    string
		appType string
		config  string
	}{
		{name: "SPA test", appType: "spa", config: `{"domains":["spa.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`},
		{name: "MPA test", appType: "mpa", config: `{"domains":["mpa.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`},
		{name: "Markdown test", appType: "mda", config: `{"domains":["markdown.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`},
	} {
		payload, marshalErr := json.Marshal(map[string]any{
			"name": testCase.name, "app_type": testCase.appType, "config": json.RawMessage(testCase.config),
		})
		if marshalErr != nil {
			t.Fatalf("marshal %s config: %v", testCase.appType, marshalErr)
		}
		createRequest = httptest.NewRequest("POST", "/jsonrpc", nil)
		createRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		if _, err = createSite(ctx, web.NewCtx(httptest.NewRecorder(), createRequest), payload); err != nil {
			t.Fatalf("sites.create for %s: %v", testCase.appType, err)
		}
	}
	if err = database.Tag("master").CallContext(ctx, "seed config update site", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('site-config', 'Config test', 'spa', 1, 1)")
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "INSERT INTO site_config_snapshots (id, site_id, revision, config_json, created_at) VALUES ('config-initial', 'site-config', 1, '{}', 1)")
		return err
	}); err != nil {
		t.Fatalf("seed config update site: %v", err)
	}
	configUpdate := api.JSONRPCApiHandlers()["sites.config.update"]
	configPayload := json.RawMessage(`{"site_id":"site-config","config":{"domains":["WWW.Example.Test."],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}}`)
	if _, err = configUpdate(ctx, web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil)), configPayload); err == nil {
		t.Fatal("sites.config.update accepted a request without owner bearer token")
	}
	configRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	configRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	configResult, err := configUpdate(ctx, web.NewCtx(httptest.NewRecorder(), configRequest), configPayload)
	if err != nil {
		t.Fatalf("sites.config.update: %v", err)
	}
	configResultJSON, err := json.Marshal(configResult)
	if err != nil {
		t.Fatalf("marshal config update response: %v", err)
	}
	var savedConfig struct {
		SiteID   string   `json:"site_id"`
		Revision int64    `json:"revision"`
		Domains  []string `json:"domains"`
		Status   string   `json:"status"`
	}
	if err = json.Unmarshal(configResultJSON, &savedConfig); err != nil {
		t.Fatalf("decode config update response: %v", err)
	}
	if savedConfig.SiteID != "site-config" || savedConfig.Revision != 2 || savedConfig.Status != "draft" || len(savedConfig.Domains) != 1 || savedConfig.Domains[0] != "www.example.test" {
		t.Fatalf("unexpected config update response: %#v", savedConfig)
	}
	configGetRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	configGetRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	configGetResult, err := api.JSONRPCApiHandlers()["sites.config.get"](ctx, web.NewCtx(httptest.NewRecorder(), configGetRequest), json.RawMessage(`{"site_id":"site-config"}`))
	if err != nil {
		t.Fatalf("sites.config.get after update: %v", err)
	}
	configGetJSON, err := json.Marshal(configGetResult)
	if err != nil {
		t.Fatalf("marshal config get response: %v", err)
	}
	var latestConfig struct {
		Revision int64 `json:"revision"`
		Config   struct {
			Domains []string `json:"domains"`
		} `json:"config"`
	}
	if err = json.Unmarshal(configGetJSON, &latestConfig); err != nil {
		t.Fatalf("decode config get response: %v", err)
	}
	if latestConfig.Revision != 2 || len(latestConfig.Config.Domains) != 1 || latestConfig.Config.Domains[0] != "www.example.test" {
		t.Fatalf("config update did not persist normalized draft: %#v", latestConfig)
	}
	if err = database.Tag("master").CallContext(ctx, "seed conflicting config domain", func(ctx context.Context, db orm.DB) error {
		if _, err := db.ExecContext(ctx, "INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES ('site-other', 'Other site', 'spa', 1, 1)"); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, "INSERT INTO site_domains (domain, site_id, created_at) VALUES ('taken.example.test', 'site-other', 1)")
		return err
	}); err != nil {
		t.Fatalf("seed conflicting domain: %v", err)
	}
	conflictingConfig := json.RawMessage(`{"site_id":"site-config","config":{"domains":["taken.example.test"],"root":".","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}}`)
	conflictRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	conflictRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	if _, err = configUpdate(ctx, web.NewCtx(httptest.NewRecorder(), conflictRequest), conflictingConfig); err == nil {
		t.Fatal("sites.config.update accepted a domain owned by another site")
	} else if rpcErr, ok := err.(*rpcError); !ok || rpcErr.code != -32602 {
		t.Fatalf("domain conflict error=%v, want -32602", err)
	}
	var revisionCount, activationCount int
	if err = database.Tag("master").CallContext(ctx, "inspect config draft transaction", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database does not support row queries")
		}
		if queryErr := queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM site_config_snapshots WHERE site_id = 'site-config'").Scan(&revisionCount); queryErr != nil {
			return queryErr
		}
		return queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM site_activations WHERE site_id = 'site-config'").Scan(&activationCount)
	}); err != nil {
		t.Fatalf("inspect config draft transaction: %v", err)
	}
	if revisionCount != 2 || activationCount != 0 {
		t.Fatalf("failed update changed revisions or activated draft: revisions=%d activations=%d", revisionCount, activationCount)
	}
	const releaseContent = "/var/lib/jasta/releases/site-config/rel-1/content"
	runtimeConfig := `{"domains":["www.example.test"],"root":"` + releaseContent + `/docs","assets":{"path":"assets"},"pages":{"index":"index.html","not_found":"404.html"}}`
	if err = database.Tag("master").CallContext(ctx, "seed runtime config root", func(ctx context.Context, db orm.DB) error {
		if _, err := db.ExecContext(ctx, "UPDATE site_config_snapshots SET config_json = ? WHERE site_id = 'site-config' AND revision = 2", runtimeConfig); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `INSERT INTO releases (id, site_id, config_snapshot_id, source_kind, archive_name, content_path, status, created_at)
			SELECT 'release-config', 'site-config', id, 'upload', 'site.zip', ?, 'ready', 2
			FROM site_config_snapshots WHERE site_id = 'site-config' AND revision = 2`, releaseContent)
		return err
	}); err != nil {
		t.Fatalf("seed runtime config root: %v", err)
	}
	rootRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	rootRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rootResult, err := api.JSONRPCApiHandlers()["sites.config.get"](ctx, web.NewCtx(httptest.NewRecorder(), rootRequest), json.RawMessage(`{"site_id":"site-config"}`))
	if err != nil {
		t.Fatalf("sites.config.get for runtime root: %v", err)
	}
	rootJSON, err := json.Marshal(rootResult)
	if err != nil {
		t.Fatalf("marshal sanitized config: %v", err)
	}
	var sanitized struct {
		Config struct {
			Root string `json:"root"`
		} `json:"config"`
	}
	if err = json.Unmarshal(rootJSON, &sanitized); err != nil {
		t.Fatalf("decode sanitized config: %v", err)
	}
	if sanitized.Config.Root != "docs" || strings.Contains(string(rootJSON), releaseContent) {
		t.Fatalf("config response did not sanitize release paths: root=%q response=%s", sanitized.Config.Root, rootJSON)
	}
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
	nginxHandler := api.JSONRPCApiHandlers()["nginx.apply"]
	if _, err = nginxHandler(ctx, web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil)), json.RawMessage(`{}`)); err == nil {
		t.Fatal("nginx.apply accepted a request without owner bearer token")
	}
	nginxRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	nginxRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	nginxResult, err := nginxHandler(ctx, web.NewCtx(httptest.NewRecorder(), nginxRequest), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("nginx.apply with owner session: %v", err)
	}
	if nginxResult.(map[string]any)["admin_domain"] != "admin.example.test" || len(nginxRunner.calls) != 3 {
		t.Fatalf("nginx.apply result=%#v calls=%#v", nginxResult, nginxRunner.calls)
	}
	certificateHandler := api.JSONRPCApiHandlers()["certificates.issue_self_signed"]
	if _, err = certificateHandler(ctx, web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil)), json.RawMessage(`{"domain":"admin.example.test"}`)); err == nil {
		t.Fatal("certificates.issue_self_signed accepted a request without owner bearer token")
	}
	certificateRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	certificateRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	certificateResult, err := certificateHandler(ctx, web.NewCtx(httptest.NewRecorder(), certificateRequest), json.RawMessage(`{"domain":"admin.example.test"}`))
	if err != nil {
		t.Fatalf("certificates.issue_self_signed: %v", err)
	}
	encodedCertificate, err := json.Marshal(certificateResult)
	if err != nil {
		t.Fatalf("marshal certificate response: %v", err)
	}
	if strings.Contains(string(encodedCertificate), "private_key_path") || strings.Contains(string(encodedCertificate), "certificate_path") {
		t.Fatalf("certificate response exposed file paths: %s", encodedCertificate)
	}
	certificateJSON, _ := json.Marshal(certificateResult)
	if err = json.Unmarshal(certificateJSON, &map[string]any{}); err != nil {
		t.Fatalf("certificate response is not JSON-serializable: %v", err)
	}
	adminCertificateConfig, err := os.ReadFile(api.conf.Nginx.ConfigPath)
	if err != nil {
		t.Fatalf("read Nginx config before certificate apply: %v", err)
	}
	if strings.Contains(string(adminCertificateConfig), "listen 443 ssl") {
		t.Fatal("Nginx config used a certificate before the next apply")
	}
	certificateApplyRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	certificateApplyRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	if _, err = api.JSONRPCApiHandlers()["nginx.apply"](ctx, web.NewCtx(httptest.NewRecorder(), certificateApplyRequest), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("nginx.apply after certificate issue: %v", err)
	}
	adminCertificateConfig, err = os.ReadFile(api.conf.Nginx.ConfigPath)
	if err != nil {
		t.Fatalf("read Nginx config after certificate apply: %v", err)
	}
	if !strings.Contains(string(adminCertificateConfig), "listen 443 ssl") || !strings.Contains(string(adminCertificateConfig), "/certificate.pem") {
		t.Fatalf("nginx.apply did not load local admin certificate:\n%s", adminCertificateConfig)
	}
	letsEncryptHandler := api.JSONRPCApiHandlers()["certificates.issue_letsencrypt"]
	if _, err = letsEncryptHandler(ctx, web.NewCtx(httptest.NewRecorder(), httptest.NewRequest("POST", "/jsonrpc", nil)), json.RawMessage(`{"domain":"example.com"}`)); err == nil {
		t.Fatal("certificates.issue_letsencrypt accepted a request without owner bearer token")
	}
	letsEncryptRequest := httptest.NewRequest("POST", "/jsonrpc", nil)
	letsEncryptRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	if _, err = letsEncryptHandler(ctx, web.NewCtx(httptest.NewRecorder(), letsEncryptRequest), json.RawMessage(`{"domain":"example.com"}`)); err == nil {
		t.Fatal("certificates.issue_letsencrypt started before the server operator accepted Let’s Encrypt terms")
	} else if rpcErr, ok := err.(*rpcError); !ok || rpcErr.code != -32011 {
		t.Fatalf("terms acceptance error=%v, want -32011", err)
	}
	api.wsServer.SetEventHandler(api.authenticateWebSocket, wsAuthEvent)
	api.wsServer.SetEventHandler(api.subscribeWebSocket, wsSubscribeEvent)
	httpListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback HTTP listener is unavailable: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validWebSocketOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		api.wsServer.HandlingHTTP(w, r)
	}))
	httpServer.Listener = httpListener
	httpServer.Start()
	defer httpServer.Close()
	t.Cleanup(api.wsServer.CloseAll)
	invalidConn := openAdminWebSocket(t, httpServer.URL)
	writeAdminWSFrame(t, invalidConn, int(wsAuthEvent), wsAuthParams{AccessToken: "invalid"})
	if response := readAdminWSFrame(t, invalidConn); response.Error != "unauthorized" {
		t.Fatalf("invalid WebSocket token response=%#v", response)
	}
	_ = invalidConn.Close()

	claims, err := manager.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess(): %v", err)
	}
	jobID, _, err := api.jobs.Create(ctx, "deploy", "", claims.SessionID)
	if err != nil {
		t.Fatalf("create progress job: %v", err)
	}
	conn := openAdminWebSocket(t, httpServer.URL)
	writeAdminWSFrame(t, conn, int(wsAuthEvent), wsAuthParams{AccessToken: pair.AccessToken})
	if response := readAdminWSFrame(t, conn); response.ID != int(wsAuthEvent) || response.Error != "" {
		t.Fatalf("WebSocket auth response=%#v", response)
	}
	writeAdminWSFrame(t, conn, int(wsSubscribeEvent), wsSubscribeParams{AfterSequence: 0})
	initialReplayFrame := readAdminWSFrame(t, conn)
	var initialReplay jobs.Replay
	if initialReplayFrame.ID != int(wsSubscribeEvent) || json.Unmarshal(initialReplayFrame.Data, &initialReplay) != nil ||
		len(initialReplay.Events) != 1 || initialReplay.Events[0].JobID != jobID {
		t.Fatalf("initial WebSocket replay=%#v payload=%s", initialReplayFrame, initialReplayFrame.Data)
	}
	otherCode, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() for second session: %v", err)
	}
	otherTokens, err := manager.Login(ctx, database.Tag("master"), "long-test-owner-password", otherCode)
	if err != nil {
		t.Fatalf("Login() second owner session: %v", err)
	}
	otherConn := openAdminWebSocket(t, httpServer.URL)
	writeAdminWSFrame(t, otherConn, int(wsAuthEvent), wsAuthParams{AccessToken: otherTokens.AccessToken})
	if response := readAdminWSFrame(t, otherConn); response.Error != "" {
		t.Fatalf("second session WebSocket auth response=%#v", response)
	}
	writeAdminWSFrame(t, otherConn, int(wsSubscribeEvent), wsSubscribeParams{AfterSequence: 0})
	otherReplayFrame := readAdminWSFrame(t, otherConn)
	var otherReplay jobs.Replay
	if json.Unmarshal(otherReplayFrame.Data, &otherReplay) != nil || len(otherReplay.Events) != 0 {
		t.Fatalf("different owner session received another session's replay: %#v", otherReplay)
	}
	for _, update := range []jobs.Update{
		{Stage: "download", Status: "running", Progress: 25},
		{Stage: "build", Status: "running", Progress: 70},
		{Stage: "publish", Status: "succeeded", Progress: 100},
	} {
		if _, err = api.jobs.Publish(ctx, jobID, update); err != nil {
			t.Fatalf("publish %s progress: %v", update.Stage, err)
		}
		frame := readAdminWSFrame(t, conn)
		var progress jobs.Event
		if frame.ID != int(wsJobEvent) || json.Unmarshal(frame.Data, &progress) != nil || progress.Stage != update.Stage || progress.Status != update.Status {
			t.Fatalf("live WebSocket progress frame=%#v payload=%s", frame, frame.Data)
		}
	}
	_ = otherConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err = otherConn.ReadMessage(); err == nil {
		t.Fatal("different owner session received another session's live progress")
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("reading from foreign session returned %v, want timeout", err)
	}
	_ = otherConn.Close()
	_ = conn.Close()

	reconnected := openAdminWebSocket(t, httpServer.URL)
	writeAdminWSFrame(t, reconnected, int(wsAuthEvent), wsAuthParams{AccessToken: pair.AccessToken})
	if response := readAdminWSFrame(t, reconnected); response.Error != "" {
		t.Fatalf("reconnect auth response=%#v", response)
	}
	writeAdminWSFrame(t, reconnected, int(wsSubscribeEvent), wsSubscribeParams{AfterSequence: initialReplay.Cursor})
	replayFrame := readAdminWSFrame(t, reconnected)
	var resumed jobs.Replay
	if replayFrame.ID != int(wsSubscribeEvent) || json.Unmarshal(replayFrame.Data, &resumed) != nil || len(resumed.Events) != 3 ||
		resumed.Events[2].Status != "succeeded" || resumed.Cursor <= initialReplay.Cursor {
		t.Fatalf("reconnected WebSocket replay=%#v payload=%s", replayFrame, replayFrame.Data)
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
	_ = reconnected.Close()
	loggedOutConn := openAdminWebSocket(t, httpServer.URL)
	writeAdminWSFrame(t, loggedOutConn, int(wsAuthEvent), wsAuthParams{AccessToken: pair.AccessToken})
	if response := readAdminWSFrame(t, loggedOutConn); response.Error != "unauthorized" {
		t.Fatalf("WebSocket auth after logout response=%#v", response)
	}
	_ = loggedOutConn.Close()
}

type adminNginxTestRunner struct {
	calls [][]string
}

func (r *adminNginxTestRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
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

func TestWebSocketOriginMustMatchForwardedHostAndScheme(t *testing.T) {
	for _, test := range []struct {
		name   string
		origin string
		host   string
		proto  string
		want   bool
	}{
		{name: "same-origin forwarded HTTPS", origin: "https://admin.example.test", host: "admin.example.test", proto: "https", want: true},
		{name: "different host", origin: "https://evil.example.test", host: "admin.example.test", proto: "https"},
		{name: "scheme mismatch", origin: "http://admin.example.test", host: "admin.example.test", proto: "https"},
		{name: "missing origin", host: "admin.example.test", proto: "https"},
		{name: "ambiguous forwarded proto", origin: "https://admin.example.test", host: "admin.example.test", proto: "https,http"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/ws", nil)
			request.Host = test.host
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.proto != "" {
				request.Header.Set("X-Forwarded-Proto", test.proto)
			}
			if got := validWebSocketOrigin(request); got != test.want {
				t.Fatalf("validWebSocketOrigin()=%v, want %v", got, test.want)
			}
		})
	}
}

type adminWSFrame struct {
	ID    int             `json:"e"`
	Data  json.RawMessage `json:"d"`
	Error string          `json:"err"`
}

func openAdminWebSocket(t *testing.T, baseURL string) *websocket.Conn {
	t.Helper()
	headers := make(http.Header)
	headers.Set("Origin", baseURL)
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(baseURL, "http"), headers)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("open admin WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func writeAdminWSFrame(t *testing.T, conn *websocket.Conn, id int, payload any) {
	t.Helper()
	data, err := json.Marshal(struct {
		ID   int `json:"e"`
		Data any `json:"d"`
	}{ID: id, Data: payload})
	if err != nil {
		t.Fatalf("marshal admin WebSocket frame: %v", err)
	}
	if err = conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write admin WebSocket frame: %v", err)
	}
}

func readAdminWSFrame(t *testing.T, conn *websocket.Conn) adminWSFrame {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set WebSocket read deadline: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read admin WebSocket frame: %v", err)
	}
	var frame adminWSFrame
	if err = json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("decode admin WebSocket frame: %v", err)
	}
	return frame
}
