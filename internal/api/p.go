package api

import (
	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/web/jsonrpc"
	"go.osspkg.com/goppy/v3/plugins/ws"

	"go.arwos.org/jasta/internal/api/admin"
	"go.arwos.org/jasta/internal/api/public"
	"go.arwos.org/jasta/internal/storage"
)

var (
	AdminPlugins = plugin.Inject(
		admin.Plugin,
		storage.Plugin(),
		ws.WithServer(),
		jsonrpc.WithTransport(jsonrpc.Path("/jsonrpc")),
		func(transport jsonrpc.Transport, api *admin.Api) error {
			transport.Add(api)
			return nil
		},
	)
	PublicPlugins = plugin.Inject(public.Plugin)
)
