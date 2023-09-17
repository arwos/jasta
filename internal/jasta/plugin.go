package jasta

import "github.com/osspkg/goppy/plugins"

var Plugin = plugins.Plugin{
	Config:  &Config{},
	Inject:  nil,
	Resolve: nil,
}
