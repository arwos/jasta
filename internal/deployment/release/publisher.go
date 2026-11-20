package release

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"go.arwos.org/jasta/internal/pkg/application"
	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/pkg/host"
)

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type Input struct {
	SiteID         string
	AppType        config.ApplicationType
	Config         json.RawMessage
	SourceKind     string // upload, url, or config for redirect-only releases
	ArchiveName    string
	SourceID       string // optional registry source ID; raw URLs and credentials are never persisted
	StagingDir     string // extracted archive directory; nil for redirect-only sites
	ReleasesRoot   string
	ContentGroupID *uint32
	CreatedAt      time.Time
}

type Result struct {
	ID               string    `json:"id"`
	SiteID           string    `json:"site_id"`
	ConfigSnapshotID string    `json:"config_snapshot_id"`
	ContentPath      string    `json:"content_path"`
	CreatedAt        time.Time `json:"created_at"`
}

type metadata struct {
	Version        int       `json:"version"`
	ID             string    `json:"id"`
	SiteID         string    `json:"site_id"`
	AppType        string    `json:"app_type"`
	SourceKind     string    `json:"source_kind"`
	ArchiveName    string    `json:"archive_name,omitempty"`
	SourceID       string    `json:"source_id,omitempty"`
	ManifestSHA256 string    `json:"manifest_sha256"`
	FileCount      int64     `json:"file_count"`
	TotalBytes     int64     `json:"total_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

// Create validates and builds a release. It never writes site_activations;
// activation is a separate operation. Files are finalized before DB rows are
// inserted, and removed again if persistence fails.
func Create(ctx context.Context, database orm.Stmt, in Input) (result Result, err error) {
	if database == nil {
		return result, errors.New("database statement is required")
	}
	if err = validateInput(in); err != nil {
		return result, err
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	} else {
		in.CreatedAt = in.CreatedAt.UTC()
	}
	if err = os.MkdirAll(in.ReleasesRoot, 0o750); err != nil {
		return result, fmt.Errorf("create releases directory: %w", err)
	}
	root, err := filepath.Abs(in.ReleasesRoot)
	if err != nil {
		return result, fmt.Errorf("resolve releases directory: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return result, err
	}
	finalPath := filepath.Join(root, id)
	tmpPath, err := os.MkdirTemp(root, ".jasta-release-*")
	if err != nil {
		return result, fmt.Errorf("create release staging directory: %w", err)
	}
	defer func() {
		if tmpPath != "" {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	appConfig, domains, err := decodeConfig(in.AppType, in.Config)
	if err != nil {
		return result, err
	}
	if err = checkSite(ctx, database, in.SiteID, in.AppType, domains); err != nil {
		return result, err
	}
	configJSON, err := prepareReleaseTree(tmpPath, finalPath, id, in, appConfig)
	if err != nil {
		return result, err
	}
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return result, fmt.Errorf("finalize release directory: %w", err)
	}
	tmpPath = ""

	result = Result{ID: id, SiteID: in.SiteID, ConfigSnapshotID: id + "-config", ContentPath: filepath.Join(finalPath, "content"), CreatedAt: in.CreatedAt}
	if err = persist(ctx, database, in, result, domains, string(configJSON)); err != nil {
		_ = makeWritable(finalPath)
		_ = os.RemoveAll(finalPath)
		return Result{}, err
	}
	return result, nil
}

func prepareReleaseTree(tmpPath, finalPath, id string, in Input, appConfig any) ([]byte, error) {
	contentPath := filepath.Join(tmpPath, "content")
	if in.AppType == config.RPA {
		if err := os.Mkdir(contentPath, 0o750); err != nil {
			return nil, fmt.Errorf("create empty redirect content: %w", err)
		}
	} else {
		sourceTarget := contentPath
		if in.AppType == config.MDA {
			sourceTarget = filepath.Join(tmpPath, "source")
		}
		if err := copyTree(in.StagingDir, sourceTarget); err != nil {
			return nil, fmt.Errorf("copy staged content: %w", err)
		}
		if err := buildAndValidate(in.AppType, appConfig, contentPath, filepath.Join(tmpPath, "source")); err != nil {
			return nil, err
		}
		if in.AppType == config.MDA {
			if err := os.RemoveAll(filepath.Join(tmpPath, "source")); err != nil {
				return nil, fmt.Errorf("remove markdown source from runtime release: %w", err)
			}
		}
	}
	// Persist a runtime config whose content paths point into the final release.
	if rooter, ok := appConfig.(config.Rooter); ok {
		relativeRoot, relErr := filepath.Rel(tmpPath, rooter.GetRoot())
		if relErr != nil || relativeRoot == ".." || strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("configured content root is outside release staging")
		}
		rooter.SetRoot(filepath.Join(finalPath, relativeRoot))
	}
	if mda, ok := appConfig.(*config.MarkdownApplication); ok {
		mda.Source = ""
	}
	configJSON, err := json.Marshal(appConfig)
	if err != nil {
		return nil, fmt.Errorf("encode release config: %w", err)
	}
	manifestHash, fileCount, totalBytes, err := manifest(contentPath)
	if err != nil {
		return nil, fmt.Errorf("create content manifest: %w", err)
	}
	archiveName := ""
	if in.SourceKind != "config" {
		archiveName = filepath.Base(in.ArchiveName)
	}
	meta := metadata{
		Version: 1, ID: id, SiteID: in.SiteID, AppType: string(in.AppType),
		SourceKind: in.SourceKind, ArchiveName: archiveName, SourceID: in.SourceID,
		ManifestSHA256: manifestHash, FileCount: fileCount, TotalBytes: totalBytes, CreatedAt: in.CreatedAt,
	}
	metadataJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode release metadata: %w", err)
	}
	if err = writeFile(filepath.Join(tmpPath, "config.json"), configJSON); err != nil {
		return nil, err
	}
	if err = writeFile(filepath.Join(tmpPath, "metadata.json"), metadataJSON); err != nil {
		return nil, err
	}
	if in.ContentGroupID != nil {
		if err = setGroupTree(tmpPath, *in.ContentGroupID); err != nil {
			return nil, fmt.Errorf("set release content group: %w", err)
		}
	}
	if err = freezeTree(tmpPath); err != nil {
		return nil, fmt.Errorf("make release immutable: %w", err)
	}
	return configJSON, nil
}

func setGroupTree(root string, groupID uint32) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return os.Chown(path, -1, int(groupID))
	})
}

func validateInput(in Input) error {
	if strings.TrimSpace(in.SiteID) == "" || in.ReleasesRoot == "" {
		return errors.New("site ID and releases root are required")
	}
	if len(in.SourceID) > 128 || strings.ContainsAny(in.SourceID, "/\\?#@:\n\r") {
		return errors.New("source ID contains invalid characters")
	}
	if in.AppType != config.SPA && in.AppType != config.MPA && in.AppType != config.MDA && in.AppType != config.RPA {
		return fmt.Errorf("unsupported application type %q", in.AppType)
	}
	if in.AppType == config.RPA {
		if in.SourceKind != "config" {
			return errors.New("redirect application release source kind must be config")
		}
		if in.StagingDir != "" {
			return errors.New("redirect application does not accept content archives")
		}
	} else {
		if in.SourceKind != "upload" && in.SourceKind != "url" {
			return errors.New("source kind must be upload or url")
		}
		if strings.TrimSpace(in.ArchiveName) == "" || filepath.Base(in.ArchiveName) == "." {
			return errors.New("archive name is required for content releases")
		}
		if in.StagingDir == "" {
			return errors.New("content archive staging directory is required")
		}
	}
	return nil
}

func decodeConfig(appType config.ApplicationType, raw []byte) (any, []string, error) {
	app, err := config.GetApplicationConfigByType(appType)
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(app); err != nil {
		return nil, nil, fmt.Errorf("decode %s config: %w", appType, err)
	}
	if err = ensureEOF(decoder); err != nil {
		return nil, nil, fmt.Errorf("decode %s config: %w", appType, err)
	}
	if validator, ok := app.(config.Validater); ok {
		if err = validator.Validate(); err != nil {
			return nil, nil, fmt.Errorf("validate %s config: %w", appType, err)
		}
	}
	domains := appDomains(app)
	if appType == config.RPA {
		domains = make([]string, 0)
		seenRedirectDomains := make(map[string]struct{})
		for _, redirect := range app.(*config.RedirectPageApplication).Redirects {
			normalized, normErr := host.Normalize(redirect.Domain)
			if normErr != nil {
				return nil, nil, fmt.Errorf("invalid config domain %q: %w", redirect.Domain, normErr)
			}
			if _, exists := seenRedirectDomains[normalized]; !exists {
				domains = append(domains, normalized)
				seenRedirectDomains[normalized] = struct{}{}
			}
		}
	}
	seen := make(map[string]struct{}, len(domains))
	for i, domain := range domains {
		normalized, normErr := host.Normalize(domain)
		if normErr != nil {
			return nil, nil, fmt.Errorf("invalid config domain %q: %w", domain, normErr)
		}
		if _, exists := seen[normalized]; exists {
			return nil, nil, fmt.Errorf("duplicate config domain %q", normalized)
		}
		seen[normalized] = struct{}{}
		domains[i] = normalized
	}
	sort.Strings(domains)
	return app, domains, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func appDomains(app any) []string {
	switch value := app.(type) {
	case *config.SinglePageApplication:
		return append([]string(nil), value.Domains...)
	case *config.MultiPageApplication:
		return append([]string(nil), value.Domains...)
	case *config.MarkdownApplication:
		return append([]string(nil), value.Domains...)
	default:
		return nil
	}
}

func checkSite(ctx context.Context, database orm.Stmt, siteID string, appType config.ApplicationType, domains []string) error {
	return database.CallContext(ctx, "validate release site", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT app_type FROM sites WHERE id = ?", siteID)
		if err != nil {
			return fmt.Errorf("query site: %w", err)
		}
		if !rows.Next() {
			_ = rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("site %q does not exist", siteID)
		}
		var storedType string
		if err = rows.Scan(&storedType); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if storedType != string(appType) {
			return fmt.Errorf("site %q uses app type %q, not %q", siteID, storedType, appType)
		}
		rows, err = db.QueryContext(ctx, "SELECT domain FROM site_domains WHERE site_id = ? ORDER BY domain", siteID)
		if err != nil {
			return fmt.Errorf("query site domains: %w", err)
		}
		registered := make([]string, 0)
		for rows.Next() {
			var domain string
			if err = rows.Scan(&domain); err != nil {
				_ = rows.Close()
				return err
			}
			registered = append(registered, domain)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if !sameStrings(registered, domains) {
			return fmt.Errorf("config domains %v do not match registered site domains %v", domains, registered)
		}
		return nil
	})
}

func persist(ctx context.Context, database orm.Stmt, in Input, result Result, domains []string, configJSON string) error {
	return database.CallContext(ctx, "save immutable release", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var appType string
		if err = tx.QueryRowContext(ctx, "SELECT app_type FROM sites WHERE id = ?", in.SiteID).Scan(&appType); err != nil {
			return fmt.Errorf("load site: %w", err)
		}
		if appType != string(in.AppType) {
			return fmt.Errorf("site %q uses app type %q, not %q", in.SiteID, appType, in.AppType)
		}
		rows, err := tx.QueryContext(ctx, "SELECT domain FROM site_domains WHERE site_id = ? ORDER BY domain", in.SiteID)
		if err != nil {
			return fmt.Errorf("load site domains: %w", err)
		}
		registered := make([]string, 0)
		for rows.Next() {
			var domain string
			if err = rows.Scan(&domain); err != nil {
				_ = rows.Close()
				return err
			}
			registered = append(registered, domain)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if !sameStrings(registered, domains) {
			return fmt.Errorf("config domains %v do not match registered site domains %v", domains, registered)
		}
		var revision int64
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision), 0) + 1 FROM site_config_snapshots WHERE site_id = ?", in.SiteID).Scan(&revision); err != nil {
			return fmt.Errorf("allocate config revision: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO site_config_snapshots (id, site_id, revision, config_json, created_at) VALUES (?, ?, ?, ?, ?)", result.ConfigSnapshotID, in.SiteID, revision, configJSON, in.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("save config snapshot: %w", err)
		}
		archiveName := ""
		if in.SourceKind != "config" {
			archiveName = filepath.Base(in.ArchiveName)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO releases (id, site_id, config_snapshot_id, source_kind, archive_name, content_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, 'ready', ?)", result.ID, in.SiteID, result.ConfigSnapshotID, in.SourceKind, archiveName, result.ContentPath, in.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("save release: %w", err)
		}
		return tx.Commit()
	})
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func buildAndValidate(appType config.ApplicationType, app any, content, source string) error {
	rooter, ok := app.(config.Rooter)
	if !ok {
		return fmt.Errorf("%s config has no content root", appType)
	}
	rootValue := rooter.GetRoot()
	if filepath.IsAbs(rootValue) || filepath.Clean(rootValue) == ".." || strings.HasPrefix(filepath.Clean(rootValue), ".."+string(filepath.Separator)) {
		return fmt.Errorf("content root %q must stay inside the archive", rootValue)
	}
	root := filepath.Join(content, filepath.Clean(rootValue))
	rooter.SetRoot(root)
	if appType == config.MDA {
		mda := app.(*config.MarkdownApplication)
		mda.Source = source
		if err := application.BuildMarkdownSite(mda); err != nil {
			return fmt.Errorf("build markdown site: %w", err)
		}
		mda.Source = ""
		return validateStaticFiles(mda)
	}
	if err := validateStaticFiles(app); err != nil {
		return err
	}
	service, err := application.Initialize(app)
	if err != nil {
		return fmt.Errorf("initialize %s site: %w", appType, err)
	}
	service.Close()
	return nil
}

func validateStaticFiles(app any) error {
	var root string
	var pages config.Pages
	switch value := app.(type) {
	case *config.SinglePageApplication:
		root, pages = value.Root, value.Pages
	case *config.MultiPageApplication:
		root, pages = value.Root, value.Pages
	case *config.MarkdownApplication:
		root, pages = value.Root, value.Pages
	}
	for _, name := range []string{pages.Index, pages.NotFound} {
		if filepath.IsAbs(name) || filepath.Clean(name) == ".." || strings.HasPrefix(filepath.Clean(name), ".."+string(filepath.Separator)) {
			return fmt.Errorf("page path %q must stay inside the content root", name)
		}
		info, err := os.Stat(filepath.Join(root, filepath.Clean(name)))
		if err != nil {
			return fmt.Errorf("required page %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required page %q is not a regular file", name)
		}
	}
	return nil
}

func copyTree(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect staging directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("staging path is not a directory")
	}
	if err = os.MkdirAll(destination, 0o750); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), link))
			if filepath.IsAbs(link) || resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
				return fmt.Errorf("staging symlink %q escapes the archive", rel)
			}
			return os.Symlink(link, target)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported staged file type %q", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		readCloseErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return readCloseErr
	})
}

func manifest(root string) (string, int64, int64, error) {
	hasher := sha256.New()
	var count, total int64
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink == 0 && !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported content type %q", path)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}
	sort.Strings(files)
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", 0, 0, err
		}
		entryInfo, err := os.Lstat(path)
		if err != nil {
			return "", 0, 0, err
		}
		if entryInfo.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", 0, 0, err
			}
			_, _ = fmt.Fprintf(hasher, "link\x00%s\x00%s\n", filepath.ToSlash(rel), filepath.ToSlash(target))
			count++
			total += int64(len(target))
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", 0, 0, err
		}
		fileHash := sha256.New()
		file, err := os.Open(path)
		if err != nil {
			return "", 0, 0, err
		}
		_, copyErr := io.Copy(fileHash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", 0, 0, copyErr
		}
		if closeErr != nil {
			return "", 0, 0, closeErr
		}
		_, _ = fmt.Fprintf(hasher, "file\x00%s\x00%d\x00%s\n", filepath.ToSlash(rel), info.Size(), hex.EncodeToString(fileHash.Sum(nil)))
		count++
		total += info.Size()
	}
	return hex.EncodeToString(hasher.Sum(nil)), count, total, nil
}

func writeFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o440); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func freezeTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		mode := fs.FileMode(0o440)
		if entry.IsDir() {
			mode = 0o550
		}
		return os.Chmod(path, mode)
	})
}

func makeWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		mode := fs.FileMode(0o700)
		if !entry.IsDir() {
			mode = 0o600
		}
		return os.Chmod(path, mode)
	})
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate release ID: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
