package command

import (
	"github.com/osspkg/go-sdk/console"
	"github.com/osspkg/jasta/internal/spiderweb"
)

func PreRenderStaticWebsites() {
	err := spiderweb.New().Run()
	console.FatalIfErr(err, "grab web")
	console.Infof("Done")
}
