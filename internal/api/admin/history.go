package admin

import (
	"context"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/deployment/release"
)

func HistoryCommand(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("history", "Show site releases, current pair, and disk usage")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.String("site", "Site ID")
		flags.StringVar("database", "/var/lib/jasta/admin.sqlite", "SQLite file used by jasta admin")
		flags.StringVar("releases", "/var/lib/jasta/releases", "Directory containing immutable releases")
	})
	setter.ExecFunc(func(siteID, databasePath, releasesRoot string) {
		database, err := openAdminDatabase(ctx, databasePath)
		console.FatalIfErr(err, "open admin database")
		defer database.Close()
		history, err := release.History(ctx, database.Tag("master"), siteID, releasesRoot)
		console.FatalIfErr(err, "read release history")
		console.Infof("Site %s: current release=%s config=%s revision=%d; total disk usage=%d bytes",
			history.SiteID, history.CurrentReleaseID, history.CurrentConfigSnapshotID, history.CurrentConfigRevision, history.TotalBytes)
		for _, record := range history.Releases {
			active := ""
			if record.Active {
				active = " [active]"
			}
			console.Infof("%s%s status=%s config=%s@%d source=%s archive=%s created=%s disk=%d bytes",
				record.ID, active, record.Status, record.ConfigSnapshotID, record.ConfigRevision,
				record.SourceKind, record.ArchiveName, record.CreatedAt.Format("2006-01-02 15:04:05 UTC"), record.DiskBytes)
		}
	})
}
