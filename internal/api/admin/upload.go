package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/auth/session"
	"go.arwos.org/jasta/internal/deployment/release"
	"go.arwos.org/jasta/internal/jobs"
	"go.arwos.org/jasta/internal/pkg/archive"
	"go.arwos.org/jasta/internal/pkg/config"
)

const (
	maxMultipartOverhead = 2 << 20
	maxUploadConfigBytes = 1 << 20
)

var uploadSiteIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

type uploadResponse struct {
	JobID   string          `json:"job_id"`
	Release uploadedRelease `json:"release"`
}

type uploadedRelease struct {
	ID               string    `json:"id"`
	SiteID           string    `json:"site_id"`
	ConfigSnapshotID string    `json:"config_snapshot_id"`
	CreatedAt        time.Time `json:"created_at"`
}

func (a *Api) uploadArchive(ctx web.Ctx) {
	request := ctx.Request()
	claims, err := a.authenticateHTTPBearer(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		ctx.Error(http.StatusUnauthorized, err)
		return
	}
	if a.jobs == nil {
		ctx.Error(http.StatusInternalServerError, errors.New("upload service is unavailable"))
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		ctx.Error(http.StatusBadRequest, errors.New("multipart form data is required"))
		return
	}
	limits := a.conf.Archive
	limits.SetDefaults()
	if err = limits.Validate(); err != nil || limits.MaxArchiveBytes > math.MaxInt64-maxMultipartOverhead {
		ctx.Error(http.StatusInternalServerError, errors.New("archive upload limits are invalid"))
		return
	}
	request.Body = http.MaxBytesReader(ctx.Response(), request.Body, limits.MaxArchiveBytes+maxMultipartOverhead)
	if err = request.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			ctx.Error(http.StatusRequestEntityTooLarge, errors.New("multipart request exceeds configured limit"))
		} else {
			ctx.Error(http.StatusBadRequest, errors.New("invalid multipart request"))
		}
		return
	}
	if request.MultipartForm != nil {
		defer func() { _ = request.MultipartForm.RemoveAll() }()
	}
	if err = validateUploadForm(request.MultipartForm); err != nil {
		ctx.Error(http.StatusBadRequest, err)
		return
	}
	siteID := request.MultipartForm.Value["site_id"][0]
	if !uploadSiteIDPattern.MatchString(siteID) {
		ctx.Error(http.StatusBadRequest, errors.New("invalid site ID"))
		return
	}
	configData := []byte(request.MultipartForm.Value["config"][0])
	if len(configData) == 0 || len(configData) > maxUploadConfigBytes {
		ctx.Error(http.StatusBadRequest, errors.New("site config is missing or too large"))
		return
	}
	configData, appType, err := a.loadUploadConfig(request.Context(), siteID, configData)
	if err != nil {
		ctx.Error(http.StatusUnprocessableEntity, errors.New("site config is invalid"))
		return
	}
	if appType == config.RPA {
		ctx.Error(http.StatusUnprocessableEntity, errors.New("redirect sites do not accept content archives"))
		return
	}
	fileHeader := request.MultipartForm.File["archive"][0]
	archiveName, format, err := uploadArchiveName(fileHeader.Filename)
	if err != nil || fileHeader.Size == 0 {
		ctx.Error(http.StatusBadRequest, errors.New("archive is empty or unsupported"))
		return
	}
	if fileHeader.Size > limits.MaxArchiveBytes {
		ctx.Error(http.StatusRequestEntityTooLarge, errors.New("archive exceeds configured limit"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		ctx.Error(http.StatusBadRequest, errors.New("uploaded archive could not be opened"))
		return
	}
	defer func() { _ = file.Close() }()

	jobID, _, err := a.jobs.Create(request.Context(), "upload", siteID, claims.SessionID)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, errors.New("could not create upload job"))
		return
	}
	if _, err = a.jobs.Publish(request.Context(), jobID, jobs.Update{Stage: "received", Status: "running", Progress: 10}); err != nil {
		ctx.Error(http.StatusInternalServerError, errors.New("could not start upload job"))
		return
	}
	if _, err = a.jobs.Publish(request.Context(), jobID, jobs.Update{Stage: "validate", Status: "running", Progress: 25}); err != nil {
		ctx.Error(http.StatusInternalServerError, errors.New("could not start archive validation"))
		return
	}
	failed := func(stage string, progress int, code string) {
		_, _ = a.jobs.Publish(context.Background(), jobID, jobs.Update{Stage: stage, Status: "failed", Progress: progress, ErrorCode: code})
		ctx.JSON(http.StatusUnprocessableEntity, map[string]string{"job_id": jobID, "error": "archive deployment failed"})
	}
	stagingDir, err := archive.Extract(file, format, a.conf.ReleasesRoot, limits)
	if err != nil {
		failed("validate", 25, "invalid_archive")
		return
	}
	defer func() { _ = removeUploadStage(stagingDir) }()
	if _, err = a.jobs.Publish(request.Context(), jobID, jobs.Update{Stage: "build", Status: "running", Progress: 70}); err != nil {
		failed("build", 10, "build_failed")
		return
	}
	result, err := release.Create(request.Context(), a.database.Tag("master"), release.Input{
		SiteID: siteID, AppType: appType, Config: configData, SourceKind: "upload", ArchiveName: archiveName,
		StagingDir: stagingDir, ReleasesRoot: a.conf.ReleasesRoot, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		failed("build", 70, "build_failed")
		return
	}
	if _, err = a.jobs.Publish(request.Context(), jobID, jobs.Update{Stage: "publish", Status: "succeeded", Progress: 100}); err != nil {
		ctx.Error(http.StatusInternalServerError, errors.New("release created but upload job could not be completed"))
		return
	}
	ctx.JSON(http.StatusCreated, uploadResponse{JobID: jobID, Release: uploadedRelease{
		ID: result.ID, SiteID: result.SiteID, ConfigSnapshotID: result.ConfigSnapshotID, CreatedAt: result.CreatedAt,
	}})
}

func (a *Api) authenticateHTTPBearer(ctx context.Context, authorization string) (session.AccessClaims, error) {
	claims, err := a.ownerClaims(ctx, authorization)
	if err != nil {
		return session.AccessClaims{}, errors.New("unauthorized")
	}
	return claims, nil
}

func (a *Api) loadUploadConfig(ctx context.Context, siteID string, raw []byte) ([]byte, config.ApplicationType, error) {
	var appType config.ApplicationType
	err := a.database.Tag("master").CallContext(ctx, "validate upload target site", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		return queryer.QueryRowContext(ctx, "SELECT app_type FROM sites WHERE id = ?", siteID).Scan(&appType)
	})
	if err != nil {
		return nil, "", fmt.Errorf("load upload target site: %w", err)
	}
	app, err := config.GetApplicationConfigByType(appType)
	if err != nil {
		return nil, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(app); err != nil {
		return nil, "", err
	}
	var extra any
	if err = decoder.Decode(&extra); err == nil {
		return nil, "", errors.New("trailing site config JSON")
	} else if !errors.Is(err, io.EOF) {
		return nil, "", err
	}
	if validator, ok := app.(config.Validater); ok {
		if err = validator.Validate(); err != nil {
			return nil, "", err
		}
	}
	return raw, appType, nil
}

func validateUploadForm(form *multipart.Form) error {
	if form == nil || len(form.Value) != 2 || len(form.File) != 1 || len(form.Value["site_id"]) != 1 ||
		len(form.Value["config"]) != 1 || len(form.File["archive"]) != 1 {
		return errors.New("multipart form must contain site_id, config, and one archive file")
	}
	return nil
}

func uploadArchiveName(name string) (string, archive.Format, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "\\\x00\r\n") {
		return "", "", errors.New("invalid archive filename")
	}
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".tar.gz"):
		return name, archive.FormatTARGZ, nil
	case strings.EqualFold(filepath.Ext(name), ".zip"):
		return name, archive.FormatZIP, nil
	default:
		return "", "", errors.New("unsupported archive format")
	}
}

func removeUploadStage(path string) error {
	if path == "" {
		return nil
	}
	return os.RemoveAll(path)
}
