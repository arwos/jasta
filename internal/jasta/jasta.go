/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package jasta

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/osspkg/go-sdk/app"
	"github.com/osspkg/go-sdk/log"
	"github.com/osspkg/go-static"
	"github.com/osspkg/goppy/plugins/web"
)

type (
	Jasta struct {
		router   web.Router
		settings map[string]Setting
	}

	Setting struct {
		Root     string
		Assets   string
		Index    string
		Page404  string
		Route404 string
	}
)

func New(c WebsiteConfigs, r web.RouterPool) *Jasta {
	return &Jasta{
		settings: prepareSettings(c),
		router:   r.Main(),
	}
}

func (v *Jasta) Up(_ app.Context) error {
	v.router.Get("/", v.handler)
	v.router.Get("#", v.handler)
	return nil
}

func (v *Jasta) Down() error {
	return nil
}

func (v *Jasta) handler(ctx web.Context) {
	ctx.Response().Header().Set("server", "jasta")

	path := pathProtect(ctx.URL().Path)
	host, _, err := net.SplitHostPort(ctx.URL().Host)
	if err != nil {
		host = ctx.URL().Host
	}

	conf, ok := v.settings[host]
	if !ok {
		ctx.Response().WriteHeader(403)
		log.WithFields(log.Fields{
			"host": host,
		}).Warnf("Host not found")
		return
	}

	if path == conf.Route404 {
		doResponse(ctx.Response(), conf.Root, conf.Page404, 404, "", 404)
		return
	}

	ext := filepath.Ext(path)
	if strings.HasPrefix(path, conf.Assets) || len(ext) > 0 {
		doResponse(ctx.Response(), conf.Root, path, 200, "", 404)
		return
	}

	doResponse(ctx.Response(), conf.Root, path, 200, conf.Index, 200)
}

func prepareSettings(c []*WebsiteConfig) map[string]Setting {
	result := make(map[string]Setting, 10)
	for _, item := range c {
		for _, domain := range item.Domains {
			result[domain] = Setting{
				Root:     item.Root,
				Assets:   item.AssetsFolder,
				Index:    item.IndexFile,
				Page404:  item.Page404,
				Route404: item.Route404,
			}
		}

	}
	return result
}

func pathProtect(path string) string {
	return strings.ReplaceAll(path, "../", "/")
}

func doResponse(w http.ResponseWriter, root string, page string, code int, nextPage string, nextCode int) {
	b, err := os.ReadFile(root + "/" + page)
	if err != nil {
		if len(nextPage) == 0 {
			w.WriteHeader(404)
			return
		}
		if b, err = os.ReadFile(root + "/" + nextPage); err != nil {
			w.WriteHeader(500)
			return
		}
		code = nextCode
	}

	w.Header().Set("Content-Type", static.DetectContentType(page, b))
	w.WriteHeader(code)
	w.Write(b) //nolint: errcheck
}
