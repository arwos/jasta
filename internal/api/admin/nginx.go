package admin

import (
	"context"
	"encoding/json"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/nginx"
)

func (a *Api) applyNginx(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	if !emptyParams(raw) {
		return nil, invalidParams()
	}
	settings := a.conf.Nginx
	settings.SetDefaults()
	if err := settings.Validate(); err != nil {
		return nil, invalidParams()
	}
	var certificatePath, privateKeyPath string
	err := a.database.Tag("master").CallContext(ctx, "load admin TLS certificate paths", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT certificate_path, private_key_path FROM certificates WHERE domain = ? AND status = 'valid'", settings.AdminDomain)
		if err != nil {
			return err
		}
		if rows.Next() {
			if err = rows.Scan(&certificatePath, &privateKeyPath); err != nil {
				_ = rows.Close()
				return err
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return nil, internalError()
	}
	if certificatePath != "" && privateKeyPath != "" {
		settings.AdminCertificate = certificatePath
		settings.AdminPrivateKey = privateKeyPath
	}
	sites := make([]nginx.Site, 0)
	index := make(map[string]int)
	err = a.database.Tag("master").CallContext(ctx, "load domains for nginx configuration", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, `SELECT site_id, domain FROM site_domains ORDER BY site_id, domain`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var siteID, domain string
			if err = rows.Scan(&siteID, &domain); err != nil {
				return err
			}
			i, ok := index[siteID]
			if !ok {
				i = len(sites)
				index[siteID] = i
				sites = append(sites, nginx.Site{})
			}
			sites[i].Domains = append(sites[i].Domains, domain)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return nil, internalError()
	}
	content, err := nginx.Render(sites, settings.RenderOptions())
	if err != nil {
		return nil, invalidParams()
	}
	if err = nginx.Apply(ctx, settings, content, a.nginxRunner); err != nil {
		return nil, &rpcError{code: -32010, message: "nginx config could not be applied"}
	}
	return map[string]any{"admin_domain": settings.AdminDomain, "sites": len(sites)}, nil
}
