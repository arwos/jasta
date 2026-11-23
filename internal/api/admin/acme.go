package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/certificates"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/host"
)

type issueLetsEncryptParams struct {
	Domain string `json:"domain"`
}

func (a *Api) issueLetsEncryptCertificate(ctx context.Context, wc web.Ctx, raw json.RawMessage) (any, error) {
	var params issueLetsEncryptParams
	if decodeParams(raw, &params) != nil || params.Domain == "" {
		return nil, invalidParams()
	}
	domain, err := certificates.ValidateLetsEncryptDomain(params.Domain)
	if err != nil {
		return nil, invalidParams()
	}
	if !a.conf.ACME.AcceptTerms {
		return nil, &rpcError{code: -32011, message: "Let’s Encrypt terms of service are not accepted in server configuration"}
	}
	if a.jobs == nil {
		return nil, internalError()
	}
	claims, err := a.authenticateHTTPBearer(ctx, wc.Header().Get("Authorization"))
	if err != nil {
		return nil, unauthorized()
	}
	configured, err := a.domainConfigured(ctx, domain)
	if err != nil {
		return nil, internalError()
	}
	if !configured {
		return nil, invalidParams()
	}
	jobID, _, err := a.jobs.Create(ctx, "certificate", "", claims.SessionID)
	if err != nil {
		return nil, internalError()
	}
	if _, err = a.jobs.Publish(ctx, jobID, jobs.Update{Stage: "validate", Status: "running", Progress: 5}); err != nil {
		return nil, internalError()
	}
	go a.runLetsEncryptJob(jobID, domain)
	return map[string]string{"job_id": jobID, "domain": domain}, nil
}

func (a *Api) domainConfigured(ctx context.Context, domain string) (bool, error) {
	adminDomain, _ := host.Normalize(a.conf.Nginx.AdminDomain)
	configured := adminDomain == domain
	err := a.database.Tag("master").CallContext(ctx, "check configured ACME domain", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT domain FROM site_domains")
		if err != nil {
			return err
		}
		for rows.Next() {
			var candidate string
			if err = rows.Scan(&candidate); err != nil {
				_ = rows.Close()
				return err
			}
			if normalized, normalizeErr := host.Normalize(candidate); normalizeErr == nil && normalized == domain {
				configured = true
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	return configured, err
}

func (a *Api) runLetsEncryptJob(jobID, domain string) {
	a.certificateMu.Lock()
	defer a.certificateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	progress := 5
	publish := func(stage string, value int) error {
		if value < progress {
			value = progress
		}
		progress = value
		_, err := a.jobs.Publish(ctx, jobID, jobs.Update{Stage: stage, Status: "running", Progress: value})
		return err
	}
	fail := func(code string) {
		_, _ = a.jobs.Publish(context.Background(), jobID, jobs.Update{Stage: "failed", Status: "failed", Progress: progress, ErrorCode: code})
	}
	if _, err := a.applyNginxConfiguration(ctx); err != nil {
		fail("nginx_apply_failed")
		return
	}
	config := certificates.ACMEConfig{
		DirectoryURL: a.conf.ACME.DirectoryURL, ContactEmail: a.conf.ACME.ContactEmail, AcceptTerms: a.conf.ACME.AcceptTerms,
	}
	certificate, err := certificates.IssueLetsEncrypt(ctx, a.conf.CertificatesRoot,
		a.conf.Nginx.ChallengeRoot, domain, config, nil, publish)
	if err != nil {
		code := "acme_failed"
		if errors.Is(err, certificates.ErrInvalidPublicDomain) {
			code = "invalid_domain"
		} else if errors.Is(err, certificates.ErrTermsNotAccepted) {
			code = "terms_not_accepted"
		}
		fail(code)
		return
	}
	if err = publish("apply_nginx", 90); err != nil {
		_ = os.RemoveAll(filepath.Dir(certificate.CertificatePath))
		fail("acme_failed")
		return
	}
	a.nginxConfigMu.Lock()
	previous, err := certificates.ReplaceIssuedCertificate(ctx, a.database.Tag("master"), certificate)
	if err != nil {
		a.nginxConfigMu.Unlock()
		_ = os.RemoveAll(filepath.Dir(certificate.CertificatePath))
		fail("certificate_store_failed")
		return
	}
	if _, err = a.applyNginxConfigurationLocked(ctx); err != nil {
		restoreErr := certificates.RestoreIssuedCertificate(context.Background(), a.database.Tag("master"), certificate, previous)
		a.nginxConfigMu.Unlock()
		if restoreErr != nil {
			// Keep candidate files in place if metadata restoration failed so the DB
			// never points Nginx at files that this operation has deleted.
			fail("certificate_restore_failed")
			return
		}
		_ = os.RemoveAll(filepath.Dir(certificate.CertificatePath))
		fail("nginx_apply_failed")
		return
	}
	a.nginxConfigMu.Unlock()
	if _, err = a.jobs.Publish(ctx, jobID, jobs.Update{Stage: "complete", Status: "succeeded", Progress: 100}); err != nil {
		fail("job_update_failed")
	}
}
