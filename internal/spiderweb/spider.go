/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package spiderweb

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"go.osspkg.com/do"
	"go.osspkg.com/events"
	"go.osspkg.com/ioutils/codec"
	"go.osspkg.com/ioutils/fs"
	"go.osspkg.com/ioutils/shell"
)

type Spider struct {
	shell  shell.TShell
	config *Config
}

func New() *Spider {
	return &Spider{}
}

func (v *Spider) Run() error {
	if err := v.initConfig(); err != nil {
		return err
	}
	if err := v.initShell(); err != nil {
		return err
	}
	ctx, cncl := context.WithCancel(context.Background())
	go events.OnStopSignal(cncl)
	all, err := v.grab(ctx)
	if err != nil {
		return err
	}
	return v.buildSitemap(all)
}

func (v *Spider) initShell() error {
	v.shell = shell.New()
	return v.shell.SetShell("/bin/bash", "x", "e", "c")
}

func (v *Spider) initConfig() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("%s/%s", dir, configName)
	if !fs.FileExist(filename) {
		return fmt.Errorf("config not found in: %s", dir)
	}
	conf := &Config{}
	if err = codec.FileEncoder(filename).Decode(conf); err != nil {
		return err
	}
	v.config = conf
	return nil
}

var rex = regexp.MustCompile(`(?miU)<a .* href="(.*)".*>`)

func (v *Spider) grab(ctx context.Context) ([]string, error) {
	all := make(map[string]struct{})
	urls := []string{"/"}

	for {
		temp := make([]string, 0, 100)
		for _, uri := range urls {
			all[uri] = struct{}{}
		}
		for _, uri := range urls {
			select {
			case <-ctx.Done():
				return do.Keys[string, struct{}](all), nil

			default:
				b, err := v.getHtml(ctx, uri)
				if err != nil {
					return nil, err
				}
				dir := v.config.OutDir + uri
				if err = os.MkdirAll(dir, 0755); err != nil {
					return nil, err
				}
				if err = os.WriteFile(dir+"/index.html", b, 0755); err != nil {
					return nil, err
				}

				for _, match := range rex.FindAllSubmatch(b, -1) {
					if u, err := url.Parse(string(match[1])); err == nil {
						if len(u.Host) != 0 {
							continue
						}
						if _, ok := all[u.Path]; ok {
							continue
						}
						temp = append(temp, u.Path)
						all[u.Path] = struct{}{}
					}
				}
			}
		}
		if len(temp) == 0 {
			return do.Keys[string, struct{}](all), nil
		}
		urls = append(urls[:0], temp...)
	}
}

var (
	htmlStart = []byte("<!DOCTYPE")
	htmlEnd   = []byte("</html>")
)

func (v *Spider) getHtml(ctx context.Context, uri string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "jasta-prerend-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir) // nolint: errcheck
	v.shell.SetDir(tmpDir)
	b, err := v.shell.Call(ctx, fmt.Sprintf(runChromium, tmpDir, v.config.DevHost+"/"+strings.TrimLeft(uri, "/")))
	if err != nil {
		return nil, err
	}
	indexStart := bytes.Index(b, htmlStart)
	if indexStart == -1 {
		return nil, fmt.Errorf("fail get start HTML document")
	}
	indexEnd := bytes.LastIndex(b, htmlEnd)
	if indexEnd == -1 {
		return nil, fmt.Errorf("fail get end HTML document")
	}
	return b[indexStart : indexEnd+len(htmlEnd)], nil
}

func (v *Spider) buildSitemap(data []string) error {
	date := time.Now().Format("2006-01-02")

	buf := &bytes.Buffer{}
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	buf.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")

	for _, datum := range data {
		buf.WriteString(fmt.Sprintf("<url>"+
			"<loc>%s%s</loc>"+
			"<changefreq>daily</changefreq>"+
			"<priority>0.7</priority>"+
			"<lastmod>%s</lastmod></url>\n", v.config.Domain, datum, date))
	}

	buf.WriteString("</urlset>\n")

	return os.WriteFile(v.config.Sitemap, buf.Bytes(), 0755)
}
