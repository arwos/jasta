/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package app

import (
	"fmt"
	"go.arwos.org/jasta/internal/pkg/files"
	"go.arwos.org/jasta/internal/pkg/protect"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v2/web"
	"go.osspkg.com/logx"
	"go.osspkg.com/static"
)

const (
	mimeHTML = "text/html"
)

type (
	App struct {
		router  web.Router
		confs   WebsiteConfigs
		domains map[string]int
		files   []map[string]struct{}
	}

	Setting struct {
		Root    string
		Assets  string
		Page404 string
		Single  bool
	}
)

func New(c WebsiteConfigs, r web.RouterPool) *App {
	return &App{
		router:  r.Main(),
		confs:   c,
		domains: make(map[string]int),
		files:   make([]map[string]struct{}, len(c)),
	}
}

func (v *App) Up() error {
	for i, conf := range v.confs {
		for _, domain := range conf.Domains {
			if _, ok := v.domains[domain]; !ok {
				return fmt.Errorf("domain %s exist for root %s", domain, conf.RootFolder)
			}
			v.domains[domain] = i
		}

		allFiles, err := files.GetAll(conf.RootFolder)
		if err != nil {
			return fmt.Errorf("get all files: %w for root %s", err, conf.RootFolder)
		}

		allFilesMap := make(map[string]struct{}, len(allFiles))
		for _, file := range allFiles {
			allFilesMap[file] = struct{}{}
		}

		v.files[i] = allFilesMap

		logx.Info("Add config", "root", conf.RootFolder, "domains", conf.Domains)
	}

	v.router.Get("/", v.handler)
	v.router.Get("#", v.handler)

	return nil
}

func (v *App) Down() error {
	return nil
}

func (v *App) handler(ctx web.Context) {
	ctx.Response().Header().Set("server", "jasta")

	host, _, err := net.SplitHostPort(ctx.URL().Host)
	if err != nil {
		host = ctx.URL().Host
	}

	path := protect.FilePath(ctx.URL().Path)

	i, ok := v.domains[host]
	if !ok {
		ctx.Response().WriteHeader(523)
		logx.Warn("Host not found", "host", host)
		return
	}

	conf := v.confs[i]
	list := v.files[i]

	ext := filepath.Ext(path)
	if strings.HasPrefix(path, conf.AssetsFolder) && len(ext) > 0 {

		if _, ok := list[path]; !ok {
			ctx.Response().WriteHeader(404)
			return
		}

		doResponse(ctx.Response(), conf.RootFolder, path, 200, true)
		return
	}

	switch conf.Type {
	case TypeSPA:
		path = "/index.html"
	case TypeMPA:
		if len(ext) == 0 {
			path = strings.TrimRight(path, "/") + "/index.html"
		}
	default:
		ctx.Response().WriteHeader(500)
		return
	}

	if _, ok := list[path]; !ok {
		doResponse(ctx.Response(), conf.RootFolder, conf.Page404File, 404, false)
		return
	}

	doResponse(ctx.Response(), conf.RootFolder, path, 200, false)
}

func doResponse(w http.ResponseWriter, root string, path string, code int, cache bool) {
	fd, err := os.Open(root + path)
	if err != nil {
		w.WriteHeader(500)
		return
	}
	defer fd.Close()

	if cache {
		w.Header().Set("Cache-Control", "max-age=10800")
	}
	w.Header().Set("Content-Type", static.DetectContentType(path, nil))
	w.WriteHeader(code)

	if _, err = io.Copy(w, fd); err != nil {
		logx.Error("Write response", "err", err, "page", path, "root", root)
	}
}
