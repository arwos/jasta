package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

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
	if err := orm.NewMigrate(database, orm.NewVirtualFS(migrations())).Run(ctx); err != nil {
		return err
	}
	return migrateReleaseSourceKind(ctx, database.Tag("master"))
}

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// migrateReleaseSourceKind expands the original upload/url constraint to
// support config-only redirect releases while preserving existing rows and
// the site_activations foreign key.
func migrateReleaseSourceKind(ctx context.Context, database orm.Stmt) error {
	return database.CallContext(ctx, "migrate release source kinds", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'releases'")
		if err != nil {
			return fmt.Errorf("inspect releases schema: %w", err)
		}
		var schema string
		if rows.Next() {
			if err = rows.Scan(&schema); err != nil {
				_ = rows.Close()
				return err
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if strings.Contains(schema, "'config'") {
			return nil
		}
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		steps := []string{
			"PRAGMA defer_foreign_keys = ON",
			`CREATE TABLE releases_new (
				id TEXT NOT NULL PRIMARY KEY,
				site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
				config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id) ON DELETE RESTRICT,
				source_kind TEXT NOT NULL CHECK (source_kind IN ('upload', 'url', 'config')),
				archive_name TEXT NOT NULL,
				content_path TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('staged', 'ready', 'failed')),
				created_at INTEGER NOT NULL
			)`,
			`INSERT INTO releases_new (id, site_id, config_snapshot_id, source_kind, archive_name, content_path, status, created_at)
				SELECT id, site_id, config_snapshot_id, source_kind, archive_name, content_path, status, created_at FROM releases`,
			`CREATE TABLE site_activations_new (
				site_id TEXT NOT NULL PRIMARY KEY REFERENCES sites(id) ON DELETE RESTRICT,
				release_id TEXT NOT NULL REFERENCES releases_new(id) ON DELETE RESTRICT,
				config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id) ON DELETE RESTRICT,
				activated_at INTEGER NOT NULL
			)`,
			`INSERT INTO site_activations_new (site_id, release_id, config_snapshot_id, activated_at)
				SELECT site_id, release_id, config_snapshot_id, activated_at FROM site_activations`,
			"DROP TABLE site_activations",
			"DROP TABLE releases",
			"ALTER TABLE releases_new RENAME TO releases",
			"ALTER TABLE site_activations_new RENAME TO site_activations",
			"CREATE INDEX releases_site_created_idx ON releases(site_id, created_at DESC)",
		}
		for _, statement := range steps {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migrate release schema: %w", err)
			}
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit release schema migration: %w", err)
		}
		return nil
	})
}
