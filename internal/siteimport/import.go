package siteimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/ioutils/codec"

	"go.arwos.org/jasta/internal/pkg/config"
)

type Issue struct {
	Path   string
	Reason string
}

type Report struct {
	Examined       int
	Imported       int
	AlreadyPresent int
	Skipped        []Issue
}

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type legacyConfig struct {
	Domains      []string `yaml:"domains"`
	Root         string   `yaml:"root"`
	Page404      string   `yaml:"page404"`
	AssetsFolder string   `yaml:"assets_folder"`
	Single       *bool    `yaml:"single"`
}

func Import(ctx context.Context, dir string, database orm.Stmt) (Report, error) {
	var report Report
	root, err := filepath.Abs(dir)
	if err != nil {
		return report, fmt.Errorf("resolve config directory: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return report, fmt.Errorf("inspect config directory: %w", err)
	}
	if !info.IsDir() {
		return report, fmt.Errorf("config path %q is not a directory", dir)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("scan config directory: %w", err)
	}
	sort.Strings(files)
	report.Examined = len(files)

	for _, path := range files {
		relativePath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return report, fmt.Errorf("resolve config path %q: %w", path, relErr)
		}
		appType, appConfig, parseErr := readConfig(path)
		if parseErr != nil {
			report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: parseErr.Error()})
			continue
		}
		if rooter, ok := appConfig.(config.Rooter); ok {
			contentRoot := rooter.GetRoot()
			if contentRoot == "" {
				report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: "root is required"})
				continue
			}
			if !filepath.IsAbs(contentRoot) {
				contentRoot = filepath.Join(filepath.Dir(path), contentRoot)
			}
			contentRoot, err = filepath.Abs(contentRoot)
			if err != nil {
				report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: fmt.Sprintf("resolve root: %v", err)})
				continue
			}
			rooter.SetRoot(contentRoot)
			rootInfo, statErr := os.Stat(contentRoot)
			if statErr != nil || !rootInfo.IsDir() {
				if statErr == nil {
					statErr = fmt.Errorf("path is not a directory")
				}
				report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: fmt.Sprintf("content root %q: %v", contentRoot, statErr)})
				continue
			}
		}
		if reason := validateDomains(appConfigDomains(appConfig)); reason != "" {
			report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: reason})
			continue
		}

		configJSON, marshalErr := json.Marshal(appConfig)
		if marshalErr != nil {
			report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: fmt.Sprintf("encode config: %v", marshalErr)})
			continue
		}
		name := strings.TrimSuffix(filepath.ToSlash(relativePath), filepath.Ext(relativePath))
		result, importErr := importOne(ctx, database, name, relativePath, appType, configJSON, appConfigDomains(appConfig))
		if importErr != nil {
			return report, fmt.Errorf("import %q: %w", filepath.ToSlash(relativePath), importErr)
		}
		switch result {
		case imported:
			report.Imported++
		case alreadyPresent:
			report.AlreadyPresent++
		case skipped:
			report.Skipped = append(report.Skipped, Issue{Path: filepath.ToSlash(relativePath), Reason: "site name or domain is already assigned to another site"})
		}
	}

	return report, nil
}

func validateDomains(domains []string) string {
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		if strings.TrimSpace(domain) == "" {
			return "domains contains an empty value"
		}
		if _, exists := seen[domain]; exists {
			return fmt.Sprintf("duplicate domain %q in config", domain)
		}
		seen[domain] = struct{}{}
	}
	return ""
}

func readConfig(path string) (config.ApplicationType, any, error) {
	var base config.Base
	if err := codec.FileEncoder(path).Decode(&base); err != nil {
		return "", nil, fmt.Errorf("decode YAML: %w", err)
	}

	if base.ApplicationType == "" {
		var legacy legacyConfig
		if err := codec.FileEncoder(path).Decode(&legacy); err != nil {
			return "", nil, fmt.Errorf("decode legacy YAML: %w", err)
		}
		if legacy.Single == nil {
			return "", nil, fmt.Errorf("missing type; expected spa or mpa (legacy single flag is also missing)")
		}
		appType := config.MPA
		if *legacy.Single {
			appType = config.SPA
		}
		appConfig, _ := config.GetApplicationConfigByType(appType)
		assets := config.Assets{Path: legacy.AssetsFolder}
		pages := config.Pages{Index: "index.html", NotFound: legacy.Page404}
		switch app := appConfig.(type) {
		case *config.SinglePageApplication:
			app.Domains, app.Root, app.Assets, app.Pages = legacy.Domains, legacy.Root, assets, pages
		case *config.MultiPageApplication:
			app.Domains, app.Root, app.Assets, app.Pages = legacy.Domains, legacy.Root, assets, pages
		}
		if validator, ok := appConfig.(config.Validater); ok {
			if err := validator.Validate(); err != nil {
				return "", nil, fmt.Errorf("incomplete legacy config: %w", err)
			}
		}
		return appType, appConfig, nil
	}

	if base.ApplicationType != config.SPA && base.ApplicationType != config.MPA {
		return "", nil, fmt.Errorf("unsupported application type %q; import supports spa and mpa", base.ApplicationType)
	}
	appConfig, err := config.GetApplicationConfigByType(base.ApplicationType)
	if err != nil {
		return "", nil, err
	}
	if err = codec.FileEncoder(path).Decode(appConfig); err != nil {
		return "", nil, fmt.Errorf("decode %s config: %w", base.ApplicationType, err)
	}
	if validator, ok := appConfig.(config.Validater); ok {
		if err = validator.Validate(); err != nil {
			return "", nil, fmt.Errorf("incomplete %s config: %w", base.ApplicationType, err)
		}
	}
	return base.ApplicationType, appConfig, nil
}

func appConfigDomains(app any) []string {
	switch value := app.(type) {
	case *config.SinglePageApplication:
		return value.Domains
	case *config.MultiPageApplication:
		return value.Domains
	default:
		return nil
	}
}

type importResult uint8

const (
	imported importResult = iota + 1
	alreadyPresent
	skipped
)

func importOne(ctx context.Context, database orm.Stmt, name, relativePath string, appType config.ApplicationType, configJSON []byte, domains []string) (importResult, error) {
	hash := sha256.Sum256([]byte(filepath.ToSlash(relativePath)))
	siteID := "yaml-" + hex.EncodeToString(hash[:16])
	snapshotID := siteID + "-config-1"
	var result importResult

	err := database.CallContext(ctx, "import legacy site", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return fmt.Errorf("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		var exists int
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM sites WHERE id = ?", siteID).Scan(&exists)
		if err == nil {
			result = alreadyPresent
			return tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM sites WHERE name = ?", name).Scan(&exists)
		if err == nil {
			result = skipped
			return tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, domain := range domains {
			err = tx.QueryRowContext(ctx, "SELECT 1 FROM site_domains WHERE domain = ?", domain).Scan(&exists)
			if err == nil {
				result = skipped
				return tx.Commit()
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		now := time.Now().Unix()
		if _, err = tx.ExecContext(ctx, "INSERT INTO sites (id, name, app_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", siteID, name, appType, now, now); err != nil {
			return err
		}
		for _, domain := range domains {
			if _, err = tx.ExecContext(ctx, "INSERT INTO site_domains (domain, site_id, created_at) VALUES (?, ?, ?)", domain, siteID, now); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO site_config_snapshots (id, site_id, revision, config_json, created_at) VALUES (?, ?, 1, ?, ?)", snapshotID, siteID, string(configJSON), now); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		result = imported
		return nil
	})
	return result, err
}
