package storage

import (
	"context"
	_ "embed"

	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
)

//go:embed migrations/0001_initial.sql
var initialMigration string

func migrations() []orm.Migration {
	return []orm.Migration{{
		Tags:    []string{"master"},
		Dialect: sqlite.Name,
		Data: map[string]string{
			"0001_initial.sql": initialMigration,
		},
	}}
}

func Plugin() plugin.Kinds {
	return plugin.Inject(
		orm.WithORM(sqlite.Name),
		orm.WithMigration(migrations()...),
	)
}

func Migrate(ctx context.Context, database orm.ORM) error {
	return orm.NewMigrate(database, orm.NewVirtualFS(migrations())).Run(ctx)
}
