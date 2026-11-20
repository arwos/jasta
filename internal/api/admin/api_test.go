package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
		}},
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
