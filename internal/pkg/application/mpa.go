package application

import (
	"fmt"
	"net/http"
	"os"

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
	requestPath, err := cleanRequestPath(ctx.URL().Path)
	if err != nil {
		ctx.Response().WriteHeader(http.StatusNotFound)
		return
	}

	var cache *config.Assets
	if isAssetPath(requestPath, a.conf.Assets.Path) || isWellKnown(requestPath) || isSysFile(ctx.URL().Path) {
		cache = &config.Assets{}
		if isAssetPath(requestPath, a.conf.Assets.Path) {
			cache = &a.conf.Assets
		}
	}
	found, err := serveFile(ctx, a.fs, requestPath, http.StatusOK, cache)
	if err != nil {
		logx.Error("failed to serve MPA file", "path", requestPath, "err", err)
		ctx.Response().WriteHeader(http.StatusInternalServerError)
		return
	}
	if found {
		return
	}

	if cache != nil || hasFileExtension(requestPath) {
		respondNotFound(ctx, a.fs, a.conf.Pages.NotFound)
		return
	}

	indexPath := directoryIndex(requestPath, a.conf.Pages.Index)
	found, err = serveFile(ctx, a.fs, indexPath, http.StatusOK, nil)
	if err != nil {
		logx.Error("failed to serve MPA directory index", "file", indexPath, "err", err)
		ctx.Response().WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		respondNotFound(ctx, a.fs, a.conf.Pages.NotFound)
	}
}
