package public

import (
	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/application"
)

var Plugin = plugin.Inject(
	plugin.Kind{
		Config: &GroupConfig{},
		Inject: func(c *GroupConfig, r web.ServerPool) *Api {
			return &Api{
				conf:     c.Public,
				pool:     r,
				apps:     make([]application.Service, 0, 16),
				handlers: make(map[string]application.Service, 16),
			}
		},
	},
)
