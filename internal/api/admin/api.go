package admin

import "go.osspkg.com/goppy/v3/plugins/web"

type Api struct {
	conf Config
	pool web.ServerPool
}
