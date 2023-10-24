/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
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

	"github.com/osspkg/jasta/internal/utils"
	"go.osspkg.com/goppy/sdk/iofile"
	"go.osspkg.com/goppy/sdk/shell"
	"go.osspkg.com/goppy/sdk/syscall"
)

type Spider struct {
	shell   shell.Shell
	tempDir string
	config  *Config
}

func New() *Spider {
	return &Spider{
		tempDir: os.TempDir(),
	}
}

func (v *Spider) Run() error {
	if err := v.initConfig(); err != nil {
		return err
	}
	if err := v.initShell(); err != nil {
		return err
	}
	ctx, cncl := context.WithCancel(context.Background())
	go syscall.OnStop(cncl)
	all, err := v.grab(ctx)
	if err != nil {
		return err
	}
	return v.buildSitemap(all)
}

func (v *Spider) initShell() error {
	v.shell = shell.New()
	v.shell.SetShell("/bin/bash")
	v.shell.SetDir(v.tempDir)
	return nil
}

func (v *Spider) initConfig() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("%s/%s", dir, configName)
	if !iofile.Exist(filename) {
		return fmt.Errorf("config not found in: %s", dir)
	}
	conf := &Config{}
	if err = iofile.FileCodec(filename).Decode(conf); err != nil {
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
				return utils.Map2Slice(all), nil

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
						fmt.Println(u.Path)
						all[u.Path] = struct{}{}
					}
				}
			}
		}
		if len(temp) == 0 {
			return utils.Map2Slice(all), nil
		}
		urls = append(urls[:0], temp...)
	}
}

func (v *Spider) getHtml(ctx context.Context, uri string) ([]byte, error) {
	b, err := v.shell.Call(ctx, fmt.Sprintf(runChromium, v.tempDir, v.config.DevHost+"/"+strings.TrimLeft(uri, "/")))
	if err != nil {
		return nil, err
	}
	index := bytes.Index(b, []byte("<!DOCTYPE"))
	if index == -1 {
		return nil, fmt.Errorf("html is empty")
	}
	return b[index:], nil
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
