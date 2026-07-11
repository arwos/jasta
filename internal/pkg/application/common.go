package application

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"go.osspkg.com/goppy/v3/pkg/mime"
	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/logx"

	"go.arwos.org/jasta/internal/pkg/urlclean"

	"go.arwos.org/jasta/internal/pkg/config"
)

const (
	wellKnown = "/.well-known"
)

func Initialize(c any) (s Service, _ error) {
	switch sc := c.(type) {
	case *config.SinglePageApplication:
		s = &spa{conf: sc}
	case *config.MultiPageApplication:
		s = &mpa{conf: sc}
	case *config.MarkdownApplication:
		s = &mda{conf: sc}
	case *config.RedirectPageApplication:
		s = &rpa{conf: sc}
	default:
		return nil, fmt.Errorf("unknown application type `%T`", sc)
	}

	return s, s.setup()
}

func assetResponse(ctx web.Ctx, fs *os.Root, page string, c config.Assets) {
	page = urlclean.Clean(page)
	fi, err := fs.Open(page)
	if err != nil {
		logx.Warn("failed to read asset content", "host", ctx.URL().Host, "file", page, "err", err)
		ctx.Response().WriteHeader(http.StatusNotFound)
		return
	}
	defer func() {
		if err = fi.Close(); err != nil {
			logx.Error("failed to close asset file", "host", ctx.URL().Host, "file", page, "err", err)
		}
	}()

	ctx.Header().Set("Content-Type", mime.DetectByFilename(page))
	ctx.Header().Set("Cache-Control", "max-age="+strconv.FormatInt(int64(c.CacheTTL.Seconds()), 10))
	ctx.Response().WriteHeader(http.StatusOK)
	if _, err := io.Copy(ctx.Response(), fi); err != nil {
		logx.Error("failed to stream asset content", "host", ctx.URL().Host, "file", page, "err", err)
	}
}

func htmlResponse(ctx web.Ctx, fs *os.Root, code int, pages ...string) bool {
	var (
		fi   *os.File
		err  error
		page string
	)

	for i := 0; i < len(pages); i++ {
		page = urlclean.Clean(pages[i])
		if fi, err = fs.Open(page); err != nil {
			logx.Warn("failed to read page content", "host", ctx.URL().Host, "file", page, "err", err)
			continue
		}
		break
	}

	if fi == nil {
		return false
	}

	defer func() {
		if err = fi.Close(); err != nil {
			logx.Error("failed to close page file", "host", ctx.URL().Host, "file", page, "err", err)
		}
	}()

	ctx.Header().Set("Content-Type", mime.DetectByFilename(page))
	ctx.Response().WriteHeader(code)

	if _, err = io.Copy(ctx.Response(), fi); err != nil {
		logx.Error("failed to stream page content", "host", ctx.URL().Host, "file", page, "err", err)
	}

	return true
}

func join(prefix, postfix string) string {
	if strings.HasSuffix(prefix, postfix) {
		return prefix
	}
	return prefix + "/" + postfix
}

func isSysFile(page string) bool {
	switch page {
	case "/sitemap.xml",
		"/robots.txt",
		"/manifest.json",
		"/favicon.ico",
		"/browserconfig.xml",
		"/3rdpartylicenses.txt",
		"/BingSiteAuth.xml":
		return true
	default:
		return false
	}
}
