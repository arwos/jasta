package admin

import (
	"context"
	"encoding/json"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/host"
	"go.arwos.org/jasta/internal/pkg/nginx"
)

func (a *Api) applyNginx(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	if !emptyParams(raw) {
		return nil, invalidParams()
	}
	sites, err := a.applyNginxConfiguration(ctx)
	if err != nil {
		return nil, &rpcError{code: -32010, message: "nginx config could not be applied"}
	}
	return map[string]any{"admin_domain": a.conf.Nginx.AdminDomain, "sites": sites}, nil
}

func (a *Api) applyNginxConfiguration(ctx context.Context) (int, error) {
	a.nginxConfigMu.Lock()
	defer a.nginxConfigMu.Unlock()
	return a.applyNginxConfigurationLocked(ctx)
}

func (a *Api) applyNginxConfigurationLocked(ctx context.Context) (int, error) {
	settings := a.conf.Nginx
	settings.SetDefaults()
	if err := settings.Validate(); err != nil {
		return 0, err
	}
	adminDomain, err := host.Normalize(settings.AdminDomain)
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	err = a.database.Tag("master").CallContext(ctx, "load admin TLS certificate paths", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT certificate_path, private_key_path FROM certificates WHERE domain = ? AND status = 'valid' AND not_after > ?", adminDomain, now)
		if err != nil {
			return err
		}
		if rows.Next() {
			if err = rows.Scan(&settings.AdminCertificate, &settings.AdminPrivateKey); err != nil {
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
		return 0, err
	}
	settings.SiteCertificates = make(map[string]nginx.CertificatePaths)
	err = a.database.Tag("master").CallContext(ctx, "load public TLS certificate paths", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, `SELECT d.domain, c.certificate_path, c.private_key_path
			FROM site_domains d JOIN certificates c ON c.domain = d.domain
			WHERE c.status = 'valid' AND c.not_after > ?`, now)
		if err != nil {
			return err
		}
		for rows.Next() {
			var domain, certificate, privateKey string
			if err = rows.Scan(&domain, &certificate, &privateKey); err != nil {
				_ = rows.Close()
				return err
			}
			normalized, normalizeErr := host.Normalize(domain)
			if normalizeErr != nil {
				_ = rows.Close()
				return normalizeErr
			}
			settings.SiteCertificates[normalized] = nginx.CertificatePaths{Certificate: certificate, PrivateKey: privateKey}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return 0, err
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
				_ = rows.Close()
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
		return 0, err
	}
	content, err := nginx.Render(sites, settings.RenderOptions())
	if err != nil {
		return 0, err
	}
	if err = nginx.Apply(ctx, settings, content, a.nginxRunner); err != nil {
		return 0, err
	}
	return len(sites), nil
}
