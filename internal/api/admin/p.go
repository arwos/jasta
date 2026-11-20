package admin

import (
	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"
)

var Plugin = plugin.Inject(
	plugin.Kind{
		Config: &GroupConfig{},
		Inject: func(c *GroupConfig, r web.ServerPool, database orm.ORM) *Api {
			return &Api{
				conf:     c.Admin,
				pool:     r,
				database: database,
			}
		},
	},
)
