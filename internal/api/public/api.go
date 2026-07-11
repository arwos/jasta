package public

import (
	"fmt"
	"net"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/application"
	"go.arwos.org/jasta/internal/pkg/config"
)

type Api struct {
	conf     Config
	pool     web.ServerPool
	apps     []application.Service
	handlers map[string]application.Service
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

	configs := config.SearchApplicationConfigs(a.conf.WebSites)
	for _, cfg := range configs {
		s, err := application.Initialize(cfg)
		if err != nil {
			logx.Error("initialize application", "err", err)
			continue
		}

		for _, domain := range s.Domains() {
			a.handlers[domain] = s
		}

		a.apps = append(a.apps, s)
	}

	return nil
}

func (a *Api) Down() error {
	for _, app := range a.apps {
		app.Close()
	}

	a.apps = a.apps[:0]
	a.handlers = make(map[string]application.Service, 16)

	return nil
}

func (a *Api) handler(ctx web.Ctx) {
	ctx.Response().Header().Set("X-Service-Name", "org.arwos.jasta")

	host, _, err := net.SplitHostPort(ctx.URL().Host)
	if err != nil {
		host = ctx.URL().Host
	}

	s, ok := a.handlers[host]
	if !ok {
		ctx.Response().WriteHeader(523)
		logx.Warn("Host not found", "host", host)
		return
	}

	s.Handler(ctx)
}
