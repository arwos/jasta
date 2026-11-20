package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/deployment/release"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/pkg/config"
)

type ArchiveFetcher func(context.Context, archive.Source, archive.FetchOptions) (io.ReadCloser, error)

type createReleaseParams struct {
	SiteID   string          `json:"site_id"`
	SourceID string          `json:"source_id,omitempty"`
	Config   json.RawMessage `json:"config"`
}

type createReleaseResult struct {
	JobID     string           `json:"job_id"`
	Status    string           `json:"status"`
	ErrorCode string           `json:"error_code,omitempty"`
	Release   *uploadedRelease `json:"release,omitempty"`
}

func (a *Api) createRelease(ctx context.Context, wc web.Ctx, raw json.RawMessage) (any, error) {
	claims, err := a.ownerClaims(ctx, wc.Header().Get("Authorization"))
	if err != nil {
		return nil, err
	}
	var params createReleaseParams
	if decodeParams(raw, &params) != nil || !uploadSiteIDPattern.MatchString(params.SiteID) || len(params.Config) == 0 {
		return nil, invalidParams()
	}
	if params.SourceID != "" {
		if _, err = uuid.Parse(params.SourceID); err != nil {
			return nil, invalidParams()
		}
	}
	configData, appType, err := a.loadUploadConfig(ctx, params.SiteID, params.Config)
	if err != nil {
		return nil, &rpcError{code: -32602, message: "site configuration is invalid"}
	}
	if appType == config.RPA && params.SourceID != "" || appType != config.RPA && params.SourceID == "" {
		return nil, invalidParams()
	}
	if a.jobs == nil {
		return nil, internalError()
	}
	var source archive.Source
	var archiveName string
	var format archive.Format
	if params.SourceID != "" {
		source, err = a.LoadSource(ctx, params.SourceID)
		if err != nil {
			return nil, &rpcError{code: -32004, message: "archive source is unavailable"}
		}
		archiveName, format, err = sourceArchiveName(source.URL)
		if err != nil {
			return nil, &rpcError{code: -32602, message: "archive source URL must end in .zip or .tar.gz"}
		}
	}

	jobID, _, err := a.jobs.Create(ctx, "deploy", params.SiteID, claims.SessionID)
	if err != nil {
		return nil, internalError()
	}
	result := createReleaseResult{JobID: jobID, Status: "running"}
	publish := func(update jobs.Update) error {
		_, publishErr := a.jobs.Publish(ctx, jobID, update)
		return publishErr
	}
	fail := func(stage string, progress int, code string) createReleaseResult {
		_, _ = a.jobs.Publish(context.Background(), jobID, jobs.Update{Stage: stage, Status: "failed", Progress: progress, ErrorCode: code})
		result.Status = "failed"
		result.ErrorCode = code
		return result
	}
	if err = publish(jobs.Update{Stage: "received", Status: "running", Progress: 5}); err != nil {
		return nil, internalError()
	}

	stagingDir := ""
	if params.SourceID != "" {
		if err = publish(jobs.Update{Stage: "download", Status: "running", Progress: 15}); err != nil {
			return nil, internalError()
		}
		limits := a.conf.Archive
		limits.SetDefaults()
		fetch := a.archiveFetcher
		if fetch == nil {
			fetch = archive.Fetch
		}
		body, fetchErr := fetch(ctx, source, archive.FetchOptions{Limits: limits, Timeout: 10 * time.Minute})
		if fetchErr != nil {
			return fail("download", 15, "source_fetch_failed"), nil
		}
		defer func() { _ = body.Close() }()
		if err = publish(jobs.Update{Stage: "validate", Status: "running", Progress: 40}); err != nil {
			return nil, internalError()
		}
		stagingDir, err = archive.Extract(body, format, a.conf.ReleasesRoot, limits)
		if err != nil {
			return fail("validate", 40, "invalid_archive"), nil
		}
		defer func() { _ = removeUploadStage(stagingDir) }()
	}
	if err = publish(jobs.Update{Stage: "build", Status: "running", Progress: 65}); err != nil {
		return nil, internalError()
	}
	sourceKind := "url"
	if appType == config.RPA {
		sourceKind = "config"
	}
	releaseResult, err := release.Create(ctx, a.database.Tag("master"), release.Input{
		SiteID: params.SiteID, AppType: appType, Config: configData, SourceKind: sourceKind,
		ArchiveName: archiveName, SourceID: params.SourceID, StagingDir: stagingDir,
		ReleasesRoot: a.conf.ReleasesRoot, ContentGroupID: a.contentGroupID, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fail("build", 65, "build_failed"), nil
	}
	if err = publish(jobs.Update{Stage: "publish", Status: "running", Progress: 90}); err != nil {
		return nil, internalError()
	}
	if err = publish(jobs.Update{Stage: "publish", Status: "succeeded", Progress: 100}); err != nil {
		return nil, internalError()
	}
	result.Status = "succeeded"
	result.Release = &uploadedRelease{ID: releaseResult.ID, SiteID: releaseResult.SiteID, ConfigSnapshotID: releaseResult.ConfigSnapshotID, CreatedAt: releaseResult.CreatedAt}
	return result, nil
}

func sourceArchiveName(sourceURL string) (string, archive.Format, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", "", errors.New("invalid source URL")
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || strings.ContainsAny(name, "\\\x00\r\n") {
		return "", "", errors.New("source URL has no archive filename")
	}
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".tar.gz"):
		return name, archive.FormatTARGZ, nil
	case strings.EqualFold(path.Ext(name), ".zip"):
		return name, archive.FormatZIP, nil
	default:
		return "", "", errors.New("unsupported source archive extension")
	}
}
