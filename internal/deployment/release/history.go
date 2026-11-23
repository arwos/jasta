package release

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"
)

type HistoryResult struct {
	SiteID                  string          `json:"site_id"`
	CurrentReleaseID        string          `json:"current_release_id,omitempty"`
	CurrentConfigSnapshotID string          `json:"current_config_snapshot_id,omitempty"`
	CurrentConfigRevision   int64           `json:"current_config_revision,omitempty"`
	TotalBytes              int64           `json:"total_bytes"`
	Releases                []ReleaseRecord `json:"releases"`
}

type ReleaseRecord struct {
	ID               string    `json:"id"`
	ConfigSnapshotID string    `json:"config_snapshot_id"`
	ConfigRevision   int64     `json:"config_revision"`
	SourceKind       string    `json:"source_kind"`
	ArchiveName      string    `json:"archive_name,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	Active           bool      `json:"active"`
	DiskBytes        int64     `json:"disk_bytes"`
}

// History returns every persisted release for a site and measures the actual
// bytes in its immutable release directory, including config and metadata.
func History(ctx context.Context, database orm.Stmt, siteID, releasesRoot string) (result HistoryResult, err error) {
	if database == nil {
		return result, errors.New("database statement is required")
	}
	if siteID == "" || releasesRoot == "" {
		return result, errors.New("site ID and releases root are required")
	}
	root, err := filepath.Abs(releasesRoot)
	if err != nil {
		return result, fmt.Errorf("resolve releases directory: %w", err)
	}
	result.SiteID = siteID
	result.Releases = make([]ReleaseRecord, 0)
	err = database.CallContext(ctx, "read site release history", func(ctx context.Context, db orm.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT 1 FROM sites WHERE id = ?", siteID)
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
		if err = rows.Close(); err != nil {
			return err
		}

		rows, err = db.QueryContext(ctx, `SELECT r.id, r.config_snapshot_id, c.revision,
			r.source_kind, r.archive_name, r.status, r.created_at, r.content_path,
			CASE WHEN a.release_id = r.id AND a.config_snapshot_id = r.config_snapshot_id THEN 1 ELSE 0 END
			FROM releases r
			JOIN site_config_snapshots c ON c.id = r.config_snapshot_id AND c.site_id = r.site_id
			LEFT JOIN site_activations a ON a.site_id = r.site_id
			WHERE r.site_id = ? ORDER BY r.created_at DESC, r.id DESC`, siteID)
		if err != nil {
			return fmt.Errorf("query site releases: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var record ReleaseRecord
			var createdAt int64
			var contentPath string
			var active int
			if err = rows.Scan(&record.ID, &record.ConfigSnapshotID, &record.ConfigRevision, &record.SourceKind,
				&record.ArchiveName, &record.Status, &createdAt, &contentPath, &active); err != nil {
				return fmt.Errorf("read release history row: %w", err)
			}
			record.CreatedAt = time.Unix(createdAt, 0).UTC()
			record.Active = active == 1
			record.DiskBytes, err = releaseDiskUsage(root, record.ID, contentPath)
			if err != nil {
				return fmt.Errorf("measure release %q: %w", record.ID, err)
			}
			if record.Active {
				result.CurrentReleaseID = record.ID
				result.CurrentConfigSnapshotID = record.ConfigSnapshotID
				result.CurrentConfigRevision = record.ConfigRevision
			}
			if record.DiskBytes > math.MaxInt64-result.TotalBytes {
				return errors.New("total release disk usage overflows int64")
			}
			result.TotalBytes += record.DiskBytes
			result.Releases = append(result.Releases, record)
		}
		if err = rows.Err(); err != nil {
			return fmt.Errorf("iterate site releases: %w", err)
		}
		return nil
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return result, nil
}

func releaseDiskUsage(root, releaseID, contentPath string) (int64, error) {
	if releaseID == "" || filepath.Base(releaseID) != releaseID || releaseID == "." || releaseID == ".." {
		return 0, fmt.Errorf("invalid release ID %q", releaseID)
	}
	releaseDir := filepath.Join(root, releaseID)
	expectedContent := filepath.Join(releaseDir, "content")
	absoluteContent, err := filepath.Abs(contentPath)
	if err != nil {
		return 0, fmt.Errorf("resolve content path: %w", err)
	}
	if filepath.Clean(absoluteContent) != filepath.Clean(expectedContent) {
		return 0, fmt.Errorf("content path is outside its release directory")
	}
	for _, path := range []string{releaseDir, expectedContent} {
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return 0, statErr
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return 0, fmt.Errorf("%q is not a real directory", path)
		}
	}
	var total int64
	err = filepath.WalkDir(releaseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0 {
			return fmt.Errorf("unsupported release file type at %q", path)
		}
		if info.Size() > math.MaxInt64-total {
			return errors.New("release disk usage overflows int64")
		}
		total += info.Size()
		return nil
	})
	return total, err
}
