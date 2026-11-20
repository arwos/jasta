package activation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"go.arwos.org/jasta/internal/pkg/config"
	"go.arwos.org/jasta/internal/snapshot"
)

const revisionSetting = "active_snapshot_revision"

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type ReloadFunc func(context.Context, snapshot.Snapshot) error

// Activate updates a site's active release/config pair and asks web to replace
// its complete in-memory snapshot before committing the database transaction.
// A rejected or unavailable reload rolls the activation back.
func Activate(ctx context.Context, database orm.Stmt, siteID, releaseID string, reload ReloadFunc) (uint64, error) {
	if database == nil {
		return 0, errors.New("database statement is required")
	}
	if siteID == "" || releaseID == "" {
		return 0, errors.New("site ID and release ID are required")
	}
	if reload == nil {
		return 0, errors.New("web reload function is required")
	}
	var revision uint64
	err := database.CallContext(ctx, "activate site release", func(ctx context.Context, db orm.DB) error {
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
		var configSnapshotID string
		err = tx.QueryRowContext(ctx, `SELECT s.app_type, r.config_snapshot_id
			FROM releases r JOIN sites s ON s.id = r.site_id
			JOIN site_config_snapshots c ON c.id = r.config_snapshot_id AND c.site_id = r.site_id
			WHERE r.id = ? AND r.site_id = ? AND r.status = 'ready'`, releaseID, siteID).Scan(&appType, &configSnapshotID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ready release %q does not belong to site %q", releaseID, siteID)
		}
		if err != nil {
			return fmt.Errorf("load release: %w", err)
		}
		if _, err = config.GetApplicationConfigByType(config.ApplicationType(appType)); err != nil {
			return fmt.Errorf("site %q has unsupported app type: %w", siteID, err)
		}

		now := time.Now().Unix()
		_, err = tx.ExecContext(ctx, `INSERT INTO site_activations (site_id, release_id, config_snapshot_id, activated_at)
			VALUES (?, ?, ?, ?) ON CONFLICT(site_id) DO UPDATE SET
			release_id = excluded.release_id, config_snapshot_id = excluded.config_snapshot_id, activated_at = excluded.activated_at`,
			siteID, releaseID, configSnapshotID, now)
		if err != nil {
			return fmt.Errorf("stage site activation: %w", err)
		}

		revision, err = nextRevision(ctx, tx)
		if err != nil {
			return err
		}
		candidate, err := activeSnapshot(ctx, tx, revision)
		if err != nil {
			return err
		}
		if err = reload(ctx, candidate); err != nil {
			return fmt.Errorf("reload web: %w", err)
		}
		return tx.Commit()
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

func nextRevision(ctx context.Context, tx *sql.Tx) (uint64, error) {
	var value string
	err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", revisionSetting).Scan(&value)
	var current uint64
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read active snapshot revision: %w", err)
	}
	if err == nil {
		current, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid active snapshot revision %q: %w", value, err)
		}
	}
	if current == ^uint64(0) {
		return 0, errors.New("active snapshot revision is exhausted")
	}
	next := current + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, revisionSetting, strconv.FormatUint(next, 10), time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("save active snapshot revision: %w", err)
	}
	return next, nil
}

func activeSnapshot(ctx context.Context, tx *sql.Tx, revision uint64) (snapshot.Snapshot, error) {
	result := snapshot.Snapshot{Version: snapshot.ProtocolVersion, Revision: revision, Sites: make([]snapshot.Site, 0)}
	rows, err := tx.QueryContext(ctx, `SELECT s.app_type, c.config_json
		FROM site_activations a
		JOIN sites s ON s.id = a.site_id
		JOIN site_config_snapshots c ON c.id = a.config_snapshot_id AND c.site_id = a.site_id
		JOIN releases r ON r.id = a.release_id AND r.site_id = a.site_id
			AND r.config_snapshot_id = a.config_snapshot_id AND r.status = 'ready'
		ORDER BY s.id`)
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("load active site pairs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var appType string
		var rawConfig string
		if err = rows.Scan(&appType, &rawConfig); err != nil {
			return snapshot.Snapshot{}, fmt.Errorf("read active site pair: %w", err)
		}
		appConfig, configErr := config.GetApplicationConfigByType(config.ApplicationType(appType))
		if configErr != nil {
			return snapshot.Snapshot{}, fmt.Errorf("unsupported active app type %q: %w", appType, configErr)
		}
		if configErr = json.Unmarshal([]byte(rawConfig), appConfig); configErr != nil {
			return snapshot.Snapshot{}, fmt.Errorf("decode active %s config: %w", appType, configErr)
		}
		encoded, marshalErr := json.Marshal(appConfig)
		if marshalErr != nil {
			return snapshot.Snapshot{}, fmt.Errorf("encode active %s config: %w", appType, marshalErr)
		}
		result.Sites = append(result.Sites, snapshot.Site{Type: config.ApplicationType(appType), Config: encoded})
	}
	if err = rows.Err(); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("iterate active site pairs: %w", err)
	}
	if err = rows.Close(); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("close active site pairs: %w", err)
	}
	var activationCount int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM site_activations").Scan(&activationCount); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("count active site pairs: %w", err)
	}
	if activationCount != len(result.Sites) {
		return snapshot.Snapshot{}, fmt.Errorf("active snapshot is incomplete: loaded %d of %d site activations", len(result.Sites), activationCount)
	}
	return result, nil
}
