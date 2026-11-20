package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"go.arwos.org/jasta/internal/jobs"
)

const (
	wsAuthEvent      event.Id = 1
	wsSubscribeEvent event.Id = 2
	wsJobEvent       event.Id = 100
	maxWSClients              = 128
)

type wsAuthParams struct {
	AccessToken string `json:"access_token"`
}

type wsAuthResponse struct {
	Authenticated bool `json:"authenticated"`
}

type wsSubscribeParams struct {
	AfterSequence int64 `json:"after_sequence"`
}

type wsClientState struct {
	sessionID string
	mu        sync.Mutex
	cancel    func()
}

func (a *Api) JobManager() *jobs.Manager { return a.jobs }

func (a *Api) serveWebSocket(ctx web.Ctx) {
	if !validWebSocketOrigin(ctx.Request()) {
		ctx.Error(http.StatusForbidden, errors.New("websocket origin is not allowed"))
		return
	}
	if a.wsServer.CountConn() >= maxWSClients {
		ctx.Error(http.StatusTooManyRequests, errors.New("websocket connection limit reached"))
		return
	}
	a.wsServer.Handling(ctx)
}

func validWebSocketOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if !strings.EqualFold(parsed.Host, request.Host) {
		return false
	}
	expectedScheme := request.Header.Get("X-Forwarded-Proto")
	if expectedScheme == "" {
		if request.TLS != nil {
			expectedScheme = "https"
		} else {
			expectedScheme = "http"
		}
	}
	if strings.Contains(expectedScheme, ",") || (expectedScheme != "http" && expectedScheme != "https") {
		return false
	}
	return parsed.Scheme == expectedScheme
}

func (a *Api) authenticateWebSocket(message event.Event, meta ws.Meta) error {
	if _, exists := a.wsClients.Load(meta.ConnectID()); exists {
		return errors.New("websocket connection is already authenticated")
	}
	var params wsAuthParams
	if err := message.Decode(&params); err != nil || params.AccessToken == "" {
		return errors.New("unauthorized")
	}
	claims, err := a.auth.VerifyAccess(params.AccessToken)
	if err != nil {
		return errors.New("unauthorized")
	}
	active, err := a.auth.SessionActive(meta.Context(), a.database.Tag("master"), claims.SessionID)
	if err != nil || !active {
		return errors.New("unauthorized")
	}
	state := &wsClientState{sessionID: claims.SessionID}
	if _, loaded := a.wsClients.LoadOrStore(meta.ConnectID(), state); loaded {
		return errors.New("websocket connection is already authenticated")
	}
	meta.AddOnCloseFunc(func(clientID string) { a.removeWSClient(clientID) })
	return message.Encode(wsAuthResponse{Authenticated: true})
}

func (a *Api) subscribeWebSocket(message event.Event, meta ws.Meta) error {
	value, exists := a.wsClients.Load(meta.ConnectID())
	if !exists {
		return errors.New("unauthorized")
	}
	state := value.(*wsClientState)
	active, err := a.auth.SessionActive(meta.Context(), a.database.Tag("master"), state.sessionID)
	if err != nil || !active {
		return errors.New("unauthorized")
	}
	var params wsSubscribeParams
	if err = message.Decode(&params); err != nil || params.AfterSequence < 0 {
		return errors.New("invalid progress cursor")
	}
	replay, cancel, err := a.jobs.Subscribe(meta.Context(), state.sessionID, params.AfterSequence, func(item jobs.Event) {
		active, activeErr := a.auth.SessionActive(context.Background(), a.database.Tag("master"), state.sessionID)
		if activeErr != nil || !active {
			a.wsServer.CloseOne(meta.ConnectID())
			return
		}
		_ = a.wsServer.SendEvent(wsJobEvent, item, meta.ConnectID())
	})
	if err != nil {
		return errors.New("progress replay is unavailable")
	}
	state.mu.Lock()
	if state.cancel != nil {
		state.cancel()
	}
	state.cancel = cancel
	state.mu.Unlock()
	return message.Encode(replay)
}

func (a *Api) removeWSClient(clientID string) {
	value, ok := a.wsClients.LoadAndDelete(clientID)
	if !ok {
		return
	}
	state := value.(*wsClientState)
	state.mu.Lock()
	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	state.mu.Unlock()
}
