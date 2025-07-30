/*
 *  Copyright (c) 2023-2024 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package spiderweb

import (
	"bytes"
	"context"
	"fmt"
	"go.osspkg.com/do"
	"go.osspkg.com/events"
	"go.osspkg.com/ioutils/fs"
	"go.osspkg.com/ioutils/shell"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Spider struct {
	shell shell.TShell

	domain  string
	host    string
	execCmd string
	outDir  string
	sitemap string
}

func New() *Spider {
	return &Spider{}
}

func (v *Spider) validateArgs(outDir, execCmd, sitemap, host, domain string) error {
	if outDir == "" {
		outDir = fs.CurrentDir() + "/build-tmp"
	}

	hostUri, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("invalid host: %s", host)
	}

	domainUri, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("invalid domain: %s", host)
	}

	v.domain = domainUri.String()
	v.host = hostUri.String()
	v.execCmd = execCmd
	v.sitemap = sitemap
	v.outDir = outDir

	return nil
}

func (v *Spider) Run(outDir, execCmd, sitemap, host, domain string) (err error) {
	if err = v.validateArgs(outDir, execCmd, sitemap, host, domain); err != nil {
		return fmt.Errorf("validate args: %s", err)
	}

	if v.shell, err = setupShell(); err != nil {
		return fmt.Errorf("setup shell: %s", err)
	}

	ctx, cncl := context.WithCancel(context.Background())
	defer cncl()

	go events.OnStopSignal(cncl)

	var links []string
	if links, err = v.grab(ctx); err != nil {
		return err
	}

	if len(v.sitemap) > 0 {
		if err = generateSitemap(links, v.sitemap, v.outDir); err != nil {
			return fmt.Errorf("generate sitemap: %s", err)
		}
	}

	return
}

var rex = regexp.MustCompile(`(?miU)<a .* href="(.*)".*>`)

func (v *Spider) grab(ctx context.Context) ([]string, error) {
	links := make(map[string]struct{})
	urls := []string{"/"}
	temp := make([]string, 0, 100)

	for {
		temp = temp[:0]

		for _, uri := range urls {
			links[uri] = struct{}{}
		}

		for _, uri := range urls {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context canceled")
			default:
			}

			b, err := v.getHtml(ctx, uri)
			if err != nil {
				return nil, err
			}

			dir := v.outDir + uri
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
					temp = append(temp, u.Path)
				}
			}
		}

		temp = do.Filter[string](
			do.Unique[string](temp),
			func(value string, _ int) bool {
				_, ok := links[value]
				return ok
			},
		)

		if len(temp) == 0 {
			return do.Keys[string, struct{}](links), nil
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

	uri = strings.TrimLeft(uri, "/")
	b, err := v.shell.Call(ctx, fmt.Sprintf(runChromium, tmpDir, v.host+"/"+uri))
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
