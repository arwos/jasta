package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/session"
)

const refreshCookieName = "jasta_admin_refresh"

func setAuthResponseHeaders(ctx web.Ctx) {
	if ctx == nil {
		return
	}
	ctx.Header().Set("Cache-Control", "no-store")
	ctx.Header().Set("Pragma", "no-cache")
}

func refreshCookieFromRequest(ctx web.Ctx) (*http.Cookie, error) {
	if ctx == nil {
		return nil, errors.New("request context is missing")
	}
	return ctx.Request().Cookie(refreshCookieName)
}

func setRefreshCookie(ctx web.Ctx, tokens session.Tokens) {
	if ctx == nil {
		return
	}
	maxAge := int(time.Until(tokens.RefreshExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(ctx.Response(), &http.Cookie{
		Name:     refreshCookieName,
		Value:    tokens.RefreshToken,
		Path:     "/jsonrpc",
		Expires:  tokens.RefreshExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   requestUsesHTTPS(ctx.Request()),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(ctx web.Ctx) {
	if ctx == nil {
		return
	}
	http.SetCookie(ctx.Response(), &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/jsonrpc",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesHTTPS(ctx.Request()),
		SameSite: http.SameSiteStrictMode,
	})
}

func requestUsesHTTPS(request *http.Request) bool {
	return request.TLS != nil || strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}
