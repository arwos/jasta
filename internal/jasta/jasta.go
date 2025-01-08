package jasta

import (
	"go.osspkg.com/goppy/v2/orm"
	"go.osspkg.com/goppy/v2/web"
	"go.osspkg.com/syncing"
	"go.osspkg.com/xc"

	"go.arwos.org/jasta/internal/pkg/spool"
)

type Jasta struct {
	db    orm.ORM
	route web.RouterPool
	spool *spool.SPool
	mux   syncing.Lock
	wg    syncing.Group
}

func New(rp web.RouterPool, db orm.ORM) *Jasta {
	return &Jasta{
		db:    db,
		route: rp,
		mux:   syncing.NewLock(),
		wg:    syncing.NewGroup(),
	}
}

func (v *Jasta) Up(ctx xc.Context) error {
	v.mux.Lock(func() {
		v.spool = spool.NewSPool(ctx.Context())
	})
	go v.startup(ctx.Context())

	v.route.All(func(_ string, r web.Router) {
		r.Get("/api/load", v.AdminGetList)
		r.Post("/api/create", v.AdminCreateConfig)
	})

	return nil
}

func (v *Jasta) Down() error {
	v.mux.Lock(func() {
		if v.spool != nil {
			v.spool.Stop()
		}
	})
	v.wg.Wait()
	return nil
}
