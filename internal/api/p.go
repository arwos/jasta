package api

import (
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/api/admin"
	"go.arwos.org/jasta/internal/api/public"
)

var Plugins = plugin.Inject(
	admin.Plugin,
	public.Plugin,
)
