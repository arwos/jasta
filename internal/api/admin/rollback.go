package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/deployment/activation"
	"go.arwos.org/jasta/internal/snapshot"
)

func RollbackCommand(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("rollback", "Reactivate a previous release and its config snapshot")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.String("site", "Site ID")
		flags.String("release", "Previous ready release ID")
		flags.StringVar("database", "/var/lib/jasta/admin.sqlite", "SQLite file used by jasta admin")
		flags.StringVar("socket", "/run/jasta/web-reload.sock", "Path to the jasta web reload socket")
	})
	setter.ExecFunc(func(siteID, releaseID, databasePath, socketPath string) {
		database, err := openAdminDatabase(ctx, databasePath)
		console.FatalIfErr(err, "open admin database")
		defer database.Close()
		revision, err := activation.Rollback(ctx, database.Tag("master"), siteID, releaseID, func(_ context.Context, next snapshot.Snapshot) error {
			data, marshalErr := json.Marshal(next)
			if marshalErr != nil {
				return marshalErr
			}
			acknowledged, sendErr := snapshot.Send(socketPath, data)
			if sendErr == nil && acknowledged != next.Revision {
				return fmt.Errorf("web acknowledged snapshot revision %d, want %d", acknowledged, next.Revision)
			}
			return sendErr
		})
		console.FatalIfErr(err, "rollback release")
		console.Infof("Rolled back site %s to release %s at snapshot revision %d", siteID, releaseID, revision)
	})
}
