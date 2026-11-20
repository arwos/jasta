package public

import (
	"fmt"
	"sync"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/application"
	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/pkg/host"
	"go.arwos.org/jasta/internal/snapshot"
)

type Api struct {
	conf      Config
	pool      web.ServerPool
	mu        sync.RWMutex
	reloadMu  sync.Mutex
	state     *snapshot.Runtime
	reloadSrv *snapshot.Server
}

func (a *Api) Up() error {
	route, ok := a.pool.ByTag(a.conf.Tag)
	if !ok {
		return fmt.Errorf("web server pool `%s` not found", a.conf.Tag)
	}

	route.Use(
		web.RecoveryMiddleware(),
		web.ThrottlingMiddleware(min(a.conf.Throttling, 1)),
	)

	route.Get("/", a.handler)
	route.Get("#", a.handler)

	state := &snapshot.Runtime{
		Apps:     make([]application.Service, 0, 16),
		Handlers: make(map[string]application.Service, 16),
	}
	configs := config.SearchApplicationConfigs(a.conf.WebSites)
	for _, cfg := range configs {
		s, err := application.Initialize(cfg)
		if err != nil {
			logx.Error("initialize application", "err", err)
			continue
		}

		if err = state.AddService(s); err != nil {
			logx.Error("register site domains", "err", err)
			s.Close()
		}
	}
	if err := state.ValidateRedirectCycles(); err != nil {
		state.Close()
		return err
	}
	a.state = state

	reloadSrv, err := snapshot.Listen(a.conf.ReloadSocket, a.applySnapshot)
	if err != nil {
		state.Close()
		a.state = nil
		return err
	}
	a.reloadSrv = reloadSrv

	return nil
}

func (a *Api) Down() error {
	if a.reloadSrv != nil {
		if err := a.reloadSrv.Close(); err != nil {
			logx.Error("close reload socket", "err", err)
		}
		a.reloadSrv = nil
	}

	a.mu.Lock()
	state := a.state
	a.state = nil
	a.mu.Unlock()
	state.Close()

	return nil
}

func (a *Api) applySnapshot(next snapshot.Snapshot) error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	candidate, err := snapshot.Prepare(next)
	if err != nil {
		return err
	}

	a.mu.Lock()
	current := a.state
	if current == nil {
		a.mu.Unlock()
		candidate.Close()
		return fmt.Errorf("web runtime is stopping")
	}
	if candidate.Revision <= current.Revision {
		a.mu.Unlock()
		candidate.Close()
		return fmt.Errorf("snapshot revision %d is not newer than active revision %d", candidate.Revision, current.Revision)
	}
	a.state = candidate
	a.mu.Unlock()

	current.Close()
	logx.Info("Activated site snapshot", "revision", candidate.Revision, "sites", len(candidate.Apps))
	return nil
}

func (a *Api) handler(ctx web.Ctx) {
	ctx.Response().Header().Set("X-Service-Name", "org.arwos.jasta")

	hostName, err := host.Normalize(ctx.URL().Host)
	if err != nil {
		ctx.Response().WriteHeader(400)
		logx.Warn("Invalid request host", "host", ctx.URL().Host, "err", err)
		return
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.state == nil {
		ctx.Response().WriteHeader(503)
		return
	}
	s, ok := a.state.Handlers[hostName]
	if !ok {
		ctx.Response().WriteHeader(523)
		logx.Warn("Host not found", "host", hostName)
		return
	}

	s.Handler(ctx)
}
