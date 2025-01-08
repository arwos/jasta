package jasta

import "go.osspkg.com/goppy/v2/plugins"

var Plugins = plugins.Inject(
	plugins.Plugin{
		Inject: New,
	},
)
