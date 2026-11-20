package api

import (
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/api/admin"
	"go.arwos.org/jasta/internal/api/public"
)

var (
	AdminPlugins  = plugin.Inject(admin.Plugin)
	PublicPlugins = plugin.Inject(public.Plugin)
)
