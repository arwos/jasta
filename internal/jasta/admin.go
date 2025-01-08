package jasta

import (
	"time"

	"go.osspkg.com/goppy/v2/orm"
	"go.osspkg.com/goppy/v2/web"
)

type ConfigName struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Enable int8   `json:"enable"`
}

func (v *Jasta) AdminGetList(ctx web.Context) {
	result := make([]ConfigName, 0, 10)

	err := v.db.Tag("master").Query(ctx.Context(), "", func(q orm.Querier) {
		q.SQL(`SELECT "id", "name", "enable" FROM "servers"`)
		q.Bind(func(b orm.Scanner) error {
			item := ConfigName{}
			if err := b.Scan(&item.ID, &item.Name, &item.Enable); err != nil {
				return err
			}
			result = append(result, item)
			return nil
		})
	})
	if err != nil {
		ctx.ErrorJSON(500, err, nil)
	} else {
		ctx.JSON(200, result)
	}
}

func (v *Jasta) AdminCreateConfig(ctx web.Context) {
	var model ConfigName
	if err := ctx.BindJSON(&model); err != nil {
		ctx.ErrorJSON(500, err, nil)
		return
	}
	currTime := time.Now()
	err := v.db.Tag("master").Exec(ctx.Context(), "", func(q orm.Executor) {
		q.SQL(`INSERT INTO "servers" ("name", "enable", "created_at", "updated_at") VALUES (?, 1, ?, ?)`)
		q.Params(model.Name, currTime, currTime)
	})
	if err != nil {
		ctx.ErrorJSON(500, err, nil)
	} else {
		ctx.JSON(200, nil)
	}
}
