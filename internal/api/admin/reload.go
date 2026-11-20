package admin

import (
	"context"
	"io"
	"os"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"

	"go.arwos.org/jasta/internal/snapshot"
)

func ReloadCommand(_ context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("reload", "Send an active site snapshot to jasta web")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.String("snapshot", "Path to a JSON site snapshot")
		flags.StringVar("socket", "/run/jasta/web-reload.sock", "Path to the jasta web reload socket")
	})
	setter.ExecFunc(func(snapshotPath, socketPath string) {
		file, err := os.Open(snapshotPath)
		console.FatalIfErr(err, "open snapshot")
		defer file.Close()

		data, err := io.ReadAll(io.LimitReader(file, snapshot.MaxSnapshotBytes+1))
		console.FatalIfErr(err, "read snapshot")
		revision, err := snapshot.Send(socketPath, data)
		console.FatalIfErr(err, "reload web")
		console.Infof("Activated site snapshot revision %d", revision)
	})
}
