package application

import (
	"fmt"
	"net/http"
	"os"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/config"
)

type mda struct {
	conf *config.MarkdownApplication
	fs   *os.Root
}

func (a *mda) setup() error {
	if err := a.conf.Validate(); err != nil {
		return err
	}
	root, err := os.OpenRoot(a.conf.Root)
	if err != nil {
		return fmt.Errorf("open generated Markdown site root %q: %w", a.conf.Root, err)
	}
	a.fs = root
	return nil
}

func (a *mda) Close() {
	if a.fs == nil {
		return
	}
	if err := a.fs.Close(); err != nil {
		logx.Error("could not close Markdown content root", "root", a.conf.Root, "err", err)
	}
	a.fs = nil
}

func (a *mda) Domains() []string {
	return a.conf.Domains
}

func (a *mda) Handler(ctx web.Ctx) {
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
		logx.Error("failed to serve Markdown site file", "path", requestPath, "err", err)
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
		logx.Error("failed to serve generated Markdown page", "file", indexPath, "err", err)
		ctx.Response().WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		respondNotFound(ctx, a.fs, a.conf.Pages.NotFound)
	}
}
