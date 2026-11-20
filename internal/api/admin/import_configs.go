package admin

import (
	"context"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/siteimport"
	"go.arwos.org/jasta/internal/storage"
)

func ImportConfigsCommand(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("import-configs", "Import SPA and MPA YAML configs into SQLite (run while admin is stopped)")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.StringVar("dir", "/etc/jasta/websites", "Directory containing site YAML configs")
		flags.StringVar("database", "/var/lib/jasta/admin.sqlite", "SQLite file used by jasta admin")
	})
	setter.ExecFunc(func(dir, databasePath string) {
		database := orm.New(ctx)
		defer database.Close()
		dbConfig := &sqlite.ConfigGroup{Pool: []sqlite.Config{{
			Tags:        "master",
			File:        databasePath,
			Cache:       "private",
			Mode:        "rwc",
			Journal:     "WAL",
			LockingMode: "EXCLUSIVE",
			OtherParams: "_foreign_keys=on",
		}}}
		console.FatalIfErr(database.ApplyConfig(sqlite.Name, dbConfig), "connect database")
		console.FatalIfErr(storage.Migrate(ctx, database), "migrate database")
		report, err := siteimport.Import(ctx, dir, database.Tag("master"))
		console.FatalIfErr(err, "import site configs")
		console.Infof("YAML import: examined=%d imported=%d already-present=%d skipped=%d", report.Examined, report.Imported, report.AlreadyPresent, len(report.Skipped))
		for _, issue := range report.Skipped {
			console.Warnf("Skipped %s: %s", issue.Path, issue.Reason)
		}
	})
}
