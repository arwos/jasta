package admin

import (
	"context"
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/storage"
)

func openAdminDatabase(ctx context.Context, path string) (orm.ORM, error) {
	database := orm.New(ctx)
	config := &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: path, Cache: "private", Mode: "rwc", Journal: "WAL",
		LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}
	if err := database.ApplyConfig(sqlite.Name, config); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect admin database: %w", err)
	}
	if err := storage.Migrate(ctx, database); err != nil {
		database.Close()
		return nil, fmt.Errorf("migrate admin database: %w", err)
	}
	return database, nil
}
