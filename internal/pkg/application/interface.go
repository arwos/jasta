package application

import "go.osspkg.com/goppy/v3/plugins/web"

type Service interface {
	setup() error
	Close()
	Domains() []string
	Handler(ctx web.Ctx)
}
