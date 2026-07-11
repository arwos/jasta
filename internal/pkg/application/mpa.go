package application

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/config"
)

type mpa struct {
	conf *config.MultiPageApplication
	fs   *os.Root
}

func (a *mpa) setup() error {
	fs, err := os.OpenRoot(a.conf.Root)
	if err != nil {
		return fmt.Errorf("could not open root directory [%s]: %w", a.conf.Root, err)
	}
	a.fs = fs
	return nil
}

func (a *mpa) Close() {
	if err := a.fs.Close(); err != nil {
		logx.Error("could not close root directory [%s]: %w", a.conf.Root, err)
	}
}

func (a *mpa) Domains() []string {
	return a.conf.Domains
}

func (a *mpa) Handler(ctx web.Ctx) {
	path := ctx.URL().Path
	ext := filepath.Ext(path)

	switch {
	case strings.HasPrefix(path, a.conf.Assets.Path) && len(ext) > 0:
		assetResponse(ctx, a.fs, path, a.conf.Assets)

	case strings.HasPrefix(path, wellKnown) || isSysFile(path):
		assetResponse(ctx, a.fs, path, config.Assets{})

	case htmlResponse(ctx, a.fs, http.StatusOK, join(path, a.conf.Pages.Index)):
	case htmlResponse(ctx, a.fs, http.StatusNotFound, a.conf.Pages.NotFound):
	default:
		ctx.Response().WriteHeader(http.StatusInternalServerError)
	}
}
