package grab

import (
	"context"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"
)

func Command(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("grab", "Grab Angular Site")
	setter.ExecFunc(func() {
		console.FatalIfErr(New().Run(ctx), "failed run grab")
	})
}
