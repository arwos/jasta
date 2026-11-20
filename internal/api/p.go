package api

import (
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/api/admin"
	"go.arwos.org/jasta/internal/api/public"
	"go.arwos.org/jasta/internal/storage"
)

var (
	AdminPlugins  = plugin.Inject(admin.Plugin, storage.Plugin())
	PublicPlugins = plugin.Inject(public.Plugin)
)
