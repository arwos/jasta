package storage

import (
	_ "embed"

	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
)

//go:embed migrations/0001_initial.sql
var initialMigration string

func Plugin() plugin.Kinds {
	return plugin.Inject(
		orm.WithORM(sqlite.Name),
		orm.WithMigration(orm.Migration{
			Tags:    []string{"master"},
			Dialect: sqlite.Name,
			Data: map[string]string{
				"0001_initial.sql": initialMigration,
			},
		}),
	)
}
