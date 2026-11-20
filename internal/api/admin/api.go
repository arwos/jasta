package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.osspkg.com/encrypt/aesgcm"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/goppy/v3/plugins/web/jsonrpc"
	"go.osspkg.com/goppy/v3/plugins/ws"

	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/deployment/activation"
	"go.arwos.org/jasta/internal/deployment/release"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/snapshot"
)

type Api struct {
	conf        Config
	pool        web.ServerPool
	database    orm.ORM
	auth        *session.Manager
	secretCodec *aesgcm.Codec
	wsServer    ws.Server
	jobs        *jobs.Manager
	wsClients   sync.Map
}

func (a *Api) Up() error {
	key, err := readOwnerEncryptionKey(a.conf.MasterKeyPath)
	if err != nil {
		return fmt.Errorf("load admin authentication key: %w", err)
	}
	a.auth, err = session.New(key)
	if err != nil {
		clear(key)
		return fmt.Errorf("initialize admin authentication: %w", err)
	}
	a.secretCodec, err = aesgcm.New(key)
	clear(key)
	if err != nil {
		return fmt.Errorf("initialize registry secret encryption: %w", err)
	}
	route, ok := a.pool.ByTag(a.conf.Tag)
	if !ok {
		return fmt.Errorf("web server pool `%s` not found", a.conf.Tag)
	}
	route.Use(web.RecoveryMiddleware())
	manager, err := jobs.New(a.database.Tag("master"))
	if err != nil {
		return fmt.Errorf("initialize progress jobs: %w", err)
	}
	a.jobs = manager
	a.wsServer.SetEventHandler(a.authenticateWebSocket, wsAuthEvent)
	a.wsServer.SetEventHandler(a.subscribeWebSocket, wsSubscribeEvent)
	route.Get("/ws", a.serveWebSocket)
	return nil
}

func (a *Api) Down() error { return nil }

type rpcError struct {
	code    int64
	message string
}

func (e *rpcError) Error() string                 { return e.message }
func (e *rpcError) GetCode() int64                { return e.code }
func (e *rpcError) GetMessage() string            { return e.message }
func (e *rpcError) GetContext() map[string]string { return nil }

func invalidParams() error { return &rpcError{code: -32602, message: "invalid params"} }
func unauthorized() error  { return &rpcError{code: -32001, message: "unauthorized"} }
func internalError() error { return &rpcError{code: -32603, message: "internal error"} }

func (a *Api) RouteTags() []string { return []string{a.conf.Tag} }

func (a *Api) JSONRPCApiHandlers() map[string]jsonrpc.THandleFunc {
	return map[string]jsonrpc.THandleFunc{
		"auth.login":            a.login,
		"auth.refresh":          a.refresh,
		"auth.logout":           a.logout,
		"auth.me":               a.protect(a.me),
		"sites.list":            a.protect(a.listSites),
		"sites.get":             a.protect(a.getSite),
		"sites.config.get":      a.protect(a.getSiteConfig),
		"sites.config.validate": a.protect(a.validateSiteConfig),
		"sources.list":          a.protect(a.listSources),
		"sources.create":        a.protect(a.createSource),
		"sources.update":        a.protect(a.updateSource),
		"sources.delete":        a.protect(a.deleteSource),
		"releases.history":      a.protect(a.releaseHistory),
		"releases.activate":     a.protect(a.activateRelease),
		"releases.rollback":     a.protect(a.rollbackRelease),
		"certificates.list":     a.protect(a.listCertificates),
	}
}

func (a *Api) protect(next jsonrpc.THandleFunc) jsonrpc.THandleFunc {
	return func(ctx context.Context, wc web.Ctx, params json.RawMessage) (any, error) {
		if err := a.checkOwnerSession(ctx, wc); err != nil {
			return nil, err
		}
		return next(ctx, wc, params)
	}
}

func (a *Api) checkOwnerSession(ctx context.Context, wc web.Ctx) error {
	value := strings.Fields(wc.Header().Get("Authorization"))
	if len(value) != 2 || !strings.EqualFold(value[0], "Bearer") {
		return unauthorized()
	}
	claims, err := a.auth.VerifyAccess(value[1])
	if err != nil {
		return unauthorized()
	}
	active, err := a.auth.SessionActive(ctx, a.database.Tag("master"), claims.SessionID)
	if err != nil {
		return internalError()
	}
	if !active {
		return unauthorized()
	}
	return nil
}

type loginParams struct {
	Password     string `json:"password"`
	SecondFactor string `json:"second_factor"`
}

type refreshParams struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *Api) login(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params loginParams
	if decodeParams(raw, &params) != nil || params.Password == "" || params.SecondFactor == "" {
		return nil, invalidParams()
	}
	tokens, err := a.auth.Login(ctx, a.database.Tag("master"), params.Password, params.SecondFactor)
	if err != nil {
		if errors.Is(err, session.ErrInvalidCredentials) {
			return nil, unauthorized()
		}
		return nil, internalError()
	}
	return tokens, nil
}

func (a *Api) refresh(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params refreshParams
	if decodeParams(raw, &params) != nil || params.RefreshToken == "" {
		return nil, invalidParams()
	}
	tokens, err := a.auth.Refresh(ctx, a.database.Tag("master"), params.RefreshToken)
	if err != nil {
		if errors.Is(err, session.ErrInvalidCredentials) {
			return nil, unauthorized()
		}
		return nil, internalError()
	}
	return tokens, nil
}

func (a *Api) logout(ctx context.Context, wc web.Ctx, raw json.RawMessage) (any, error) {
	if err := a.checkOwnerSession(ctx, wc); err != nil {
		return nil, err
	}
	var params refreshParams
	if decodeParams(raw, &params) != nil || params.RefreshToken == "" {
		return nil, invalidParams()
	}
	if err := a.auth.Logout(ctx, a.database.Tag("master"), params.RefreshToken); err != nil {
		return nil, internalError()
	}
	return map[string]bool{"ok": true}, nil
}

func (a *Api) me(_ context.Context, _ web.Ctx, _ json.RawMessage) (any, error) {
	return map[string]string{"subject": "owner"}, nil
}

type siteSummary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	AppType         string    `json:"app_type"`
	Domains         []string  `json:"domains"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ActiveReleaseID string    `json:"active_release_id,omitempty"`
}

func (a *Api) listSites(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	if !emptyParams(raw) {
		return nil, invalidParams()
	}
	result := make([]siteSummary, 0)
	err := a.database.Tag("master").CallContext(ctx, "list sites for admin API", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, `SELECT s.id, s.name, s.app_type, s.created_at, s.updated_at,
			a.release_id FROM sites s LEFT JOIN site_activations a ON a.site_id = s.id ORDER BY s.name, s.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item siteSummary
			var createdAt, updatedAt int64
			var active sql.NullString
			if err := rows.Scan(&item.ID, &item.Name, &item.AppType, &createdAt, &updatedAt, &active); err != nil {
				return err
			}
			item.CreatedAt = time.Unix(createdAt, 0).UTC()
			item.UpdatedAt = time.Unix(updatedAt, 0).UTC()
			item.ActiveReleaseID = active.String
			result = append(result, item)
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
	for index := range result {
		result[index].Domains = make([]string, 0)
		err = a.database.Tag("master").CallContext(ctx, "list site domains for admin API", func(ctx context.Context, db orm.DB) error {
			domains, queryErr := db.QueryContext(ctx, "SELECT domain FROM site_domains WHERE site_id = ? ORDER BY domain", result[index].ID)
			if queryErr != nil {
				return queryErr
			}
			for domains.Next() {
				var domain string
				if queryErr = domains.Scan(&domain); queryErr != nil {
					_ = domains.Close()
					return queryErr
				}
				result[index].Domains = append(result[index].Domains, domain)
			}
			if queryErr = domains.Err(); queryErr != nil {
				_ = domains.Close()
				return queryErr
			}
			return domains.Close()
		})
		if err != nil {
			return nil, internalError()
		}
	}
	return result, nil
}

type siteIDParams struct {
	SiteID string `json:"site_id"`
}

func (a *Api) getSite(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params siteIDParams
	if decodeParams(raw, &params) != nil || params.SiteID == "" {
		return nil, invalidParams()
	}
	items, err := a.listSites(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range items.([]siteSummary) {
		if item.ID == params.SiteID {
			return item, nil
		}
	}
	return nil, &rpcError{code: -32004, message: "site not found"}
}

func (a *Api) getSiteConfig(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params siteIDParams
	if decodeParams(raw, &params) != nil || params.SiteID == "" {
		return nil, invalidParams()
	}
	var appType, rawConfig string
	var revision int64
	err := a.database.Tag("master").CallContext(ctx, "read site config for admin API", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		return queryer.QueryRowContext(ctx, `SELECT s.app_type, c.revision, c.config_json FROM sites s
			JOIN site_config_snapshots c ON c.site_id = s.id WHERE s.id = ? ORDER BY c.revision DESC LIMIT 1`, params.SiteID).
			Scan(&appType, &revision, &rawConfig)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &rpcError{code: -32004, message: "site config not found"}
	}
	if err != nil {
		return nil, internalError()
	}
	return map[string]any{"site_id": params.SiteID, "app_type": appType, "revision": revision, "config": json.RawMessage(rawConfig)}, nil
}

type validateConfigParams struct {
	AppType config.ApplicationType `json:"app_type"`
	Config  json.RawMessage        `json:"config"`
}

func (a *Api) validateSiteConfig(_ context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params validateConfigParams
	if decodeParams(raw, &params) != nil || params.AppType == "" || len(params.Config) == 0 {
		return nil, invalidParams()
	}
	value, err := config.GetApplicationConfigByType(params.AppType)
	if err != nil || json.Unmarshal(params.Config, value) != nil {
		return nil, invalidParams()
	}
	validator, ok := value.(interface{ Validate() error })
	if !ok {
		return nil, internalError()
	}
	if err = validator.Validate(); err != nil {
		return nil, invalidParams()
	}
	return map[string]any{"valid": true}, nil
}

func (a *Api) listSources(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	if !emptyParams(raw) {
		return nil, invalidParams()
	}
	type source struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		AuthType  string    `json:"auth_type"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	items := make([]source, 0)
	err := a.database.Tag("master").CallContext(ctx, "list registry sources for admin API", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT id, name, auth_type, updated_at FROM registry_sources ORDER BY name, id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var item source
			var updated int64
			if err = rows.Scan(&item.ID, &item.Name, &item.AuthType, &updated); err != nil {
				return err
			}
			item.UpdatedAt = time.Unix(updated, 0).UTC()
			items = append(items, item)
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
	return items, nil
}

type historyParams struct {
	SiteID string `json:"site_id"`
}

func (a *Api) releaseHistory(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params historyParams
	if decodeParams(raw, &params) != nil || params.SiteID == "" {
		return nil, invalidParams()
	}
	result, err := release.History(ctx, a.database.Tag("master"), params.SiteID, a.conf.ReleasesRoot)
	if err != nil {
		return nil, internalError()
	}
	return result, nil
}

type releaseParams struct {
	SiteID    string `json:"site_id"`
	ReleaseID string `json:"release_id"`
}

func (a *Api) activateRelease(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	return a.changeRelease(ctx, raw, false)
}
func (a *Api) rollbackRelease(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	return a.changeRelease(ctx, raw, true)
}
func (a *Api) changeRelease(ctx context.Context, raw json.RawMessage, rollback bool) (any, error) {
	var params releaseParams
	if decodeParams(raw, &params) != nil || params.SiteID == "" || params.ReleaseID == "" {
		return nil, invalidParams()
	}
	reload := func(_ context.Context, next snapshot.Snapshot) error {
		data, err := json.Marshal(next)
		if err != nil {
			return err
		}
		revision, err := snapshot.Send(a.conf.ReloadSocket, data)
		if err == nil && revision != next.Revision {
			return fmt.Errorf("web acknowledged unexpected snapshot revision")
		}
		return err
	}
	var revision uint64
	var err error
	if rollback {
		revision, err = activation.Rollback(ctx, a.database.Tag("master"), params.SiteID, params.ReleaseID, reload)
	} else {
		revision, err = activation.Activate(ctx, a.database.Tag("master"), params.SiteID, params.ReleaseID, reload)
	}
	if err != nil {
		return nil, &rpcError{code: -32004, message: "release activation failed"}
	}
	return map[string]any{"site_id": params.SiteID, "release_id": params.ReleaseID, "snapshot_revision": revision}, nil
}

func (a *Api) listCertificates(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	if !emptyParams(raw) {
		return nil, invalidParams()
	}
	type certificate struct {
		ID        string     `json:"id"`
		Domain    string     `json:"domain"`
		Issuer    string     `json:"issuer"`
		Status    string     `json:"status"`
		NotBefore *time.Time `json:"not_before,omitempty"`
		NotAfter  *time.Time `json:"not_after,omitempty"`
	}
	items := make([]certificate, 0)
	err := a.database.Tag("master").CallContext(ctx, "list certificates for admin API", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT id, domain, issuer, status, not_before, not_after FROM certificates ORDER BY domain")
		if err != nil {
			return err
		}
		for rows.Next() {
			var item certificate
			var before, after sql.NullInt64
			if err = rows.Scan(&item.ID, &item.Domain, &item.Issuer, &item.Status, &before, &after); err != nil {
				return err
			}
			if before.Valid {
				value := time.Unix(before.Int64, 0).UTC()
				item.NotBefore = &value
			}
			if after.Valid {
				value := time.Unix(after.Int64, 0).UTC()
				item.NotAfter = &value
			}
			items = append(items, item)
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
	return items, nil
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("params are required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("params must contain one JSON value")
	}
	return nil
}

func emptyParams(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	if string(raw) == "null" {
		return true
	}
	return decodeParams(raw, &struct{}{}) == nil
}

var _ jsonrpc.TApi = (*Api)(nil)
