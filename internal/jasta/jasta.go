/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package jasta

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.osspkg.com/do"
	"go.osspkg.com/goppy/v2/web"
	"go.osspkg.com/logx"
	"go.osspkg.com/static"
)

const (
	mimeHTML = "text/html"
)

type (
	Jasta struct {
		router   web.Router
		settings map[string]Setting
	}

	Setting struct {
		Root    string
		Assets  string
		Page404 string
		Single  bool
	}
)

func New(c WebsiteConfigs, r web.ServerPool) (*Jasta, error) {
	route, ok := r.Main()
	if !ok {
		return nil, fmt.Errorf("jasta: not found main route")
	}
	return &Jasta{
		settings: prepareSettings(c),
		router:   route,
	}, nil
}

func (v *Jasta) Up() error {
	v.router.Get("/", v.handler)
	v.router.Get("#", v.handler)
	return nil
}

func (v *Jasta) Down() error {
	return nil
}

func (v *Jasta) handler(ctx web.Ctx) {
	ctx.Response().Header().Set("server", "jasta")

	path := protect(ctx.URL().Path)
	host, _, err := net.SplitHostPort(ctx.URL().Host)
	if err != nil {
		host = ctx.URL().Host
	}

	conf, ok := v.settings[host]
	if !ok {
		ctx.Response().WriteHeader(523)
		logx.Warn("Host not found", "host", host)
		return
	}

	ext := filepath.Ext(path)
	if strings.HasPrefix(path, conf.Assets) && len(ext) > 0 {
		doResponse(ctx.Response(), conf.Root, path, "")
		return
	}

	if conf.Single {
		if len(ext) == 0 {
			path = "/index.html"
		}
	} else {
		if len(ext) == 0 {
			path = strings.TrimRight(path, "/") + "/index.html"
		}
	}
	doResponse(ctx.Response(), conf.Root, path, conf.Page404)
}

func prepareSettings(c []*WebsiteConfig) map[string]Setting {
	result := make(map[string]Setting, 10)
	for _, item := range c {
		for _, domain := range item.Domains {
			logx.Info("Load config", "domain", domain, "root", item.Root)
			result[domain] = Setting{
				Root:    item.Root,
				Assets:  item.AssetsFolder,
				Page404: do.IfElse(len(item.Page404) > 0 && item.Page404[0] == '/', item.Page404, "/"+item.Page404),
				Single:  item.Single,
			}
		}

	}
	return result
}

func protect(path string) string {
	i, j, n := 0, -1, len(path)
	in, out := []byte(path), make([]byte, 0, n)

	for i < n {
		switch true {
		case (i == 0 || i+1 >= n) && in[i] == '.':
			i++
		case in[i] == '.' && i+1 < n && (in[i+1] == '/' || in[i+1] == '.'):
			i++
		case in[i] == '/' && i+1 < n && in[i+1] == '/':
			i++
		case in[i] == '/' && i+1 < n && in[i+1] == '.':
			if j >= 0 && out[j] != '/' {
				out = append(out, in[i])
				j++
			}
			i += 2
		default:
			out = append(out, in[i])
			j++
			i++
		}
	}

	return string(out)
}

func doResponse(w http.ResponseWriter, root string, p200 string, p404 string) {
	b, err := os.ReadFile(root + do.IfElse(len(p200) > 0 && p200[0] == '/', "", "/") + p200)
	code := do.IfElse(p200 == p404, 404, 200)
	if err != nil {
		if len(p404) == 0 {
			w.WriteHeader(404)
			return
		}
		b, err = os.ReadFile(root + do.IfElse(len(p404) > 0 && p404[0] == '/', "", "/") + p404)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		code, p200 = 404, p404
	}
	mime := static.DetectContentType(p200, b)
	w.Header().Set("Content-Type", mime)
	if mime != mimeHTML {
		w.Header().Set("Cache-Control", "max-age=10800")
	}
	w.WriteHeader(code)
	if _, err = w.Write(b); err != nil {
		logx.Error("Write response", "err", err, "page", p200)
	}
}
