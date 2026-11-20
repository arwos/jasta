package admin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/certificates"
)

type issueSelfSignedParams struct {
	Domain string `json:"domain"`
}

type certificateResponse struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	Issuer    string    `json:"issuer"`
	Status    string    `json:"status"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
}

func (a *Api) issueSelfSignedCertificate(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params issueSelfSignedParams
	if decodeParams(raw, &params) != nil || params.Domain == "" {
		return nil, invalidParams()
	}
	certificate, err := certificates.CreateSelfSigned(ctx, a.database.Tag("master"), a.conf.CertificatesRoot, params.Domain, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, certificates.ErrAlreadyExists):
			return nil, &rpcError{code: -32009, message: "certificate already exists for domain"}
		case errors.Is(err, certificates.ErrInvalidLocalDomain):
			return nil, invalidParams()
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, &rpcError{code: -32010, message: "certificate creation failed"}
		default:
			return nil, internalError()
		}
	}
	return certificateResponse{
		ID: certificate.ID, Domain: certificate.Domain, Issuer: certificate.Issuer, Status: certificate.Status,
		NotBefore: certificate.NotBefore, NotAfter: certificate.NotAfter,
	}, nil
}
