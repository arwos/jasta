package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/orm"
)

const replayLimit = 200

var safeCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type Event struct {
	Sequence       int64     `json:"sequence"`
	JobID          string    `json:"job_id"`
	Kind           string    `json:"kind"`
	Stage          string    `json:"stage"`
	Status         string    `json:"status"`
	Progress       int       `json:"progress"`
	ErrorCode      string    `json:"error_code,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ownerSessionID string
}

type Update struct {
	Stage     string
	Status    string
	Progress  int
	ErrorCode string
}

type Replay struct {
	Events  []Event `json:"events"`
	Cursor  int64   `json:"cursor"`
	HasMore bool    `json:"has_more"`
}

type subscriber struct {
	id          uint64
	after       int64
	ownerFamily map[string]struct{}
	consume     func(Event)
}

type Manager struct {
	database orm.Stmt
	mu       sync.Mutex
	nextID   atomic.Uint64
	subs     map[uint64]subscriber
}

type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func New(database orm.Stmt) (*Manager, error) {
	if database == nil {
		return nil, errors.New("jobs database is required")
	}
	return &Manager{database: database, subs: make(map[uint64]subscriber)}, nil
}

// Create persists a queued job and its initial event in one transaction.
func (m *Manager) Create(ctx context.Context, kind, siteID, ownerSessionID string) (string, Event, error) {
	if !validLabel(kind) || kind == "queued" || siteID != "" && !validLabel(siteID) {
		return "", Event{}, errors.New("job kind or site ID is invalid")
	}
	if _, err := uuid.Parse(ownerSessionID); err != nil {
		return "", Event{}, errors.New("valid owner session ID is required for progress jobs")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return "", Event{}, fmt.Errorf("generate job ID: %w", err)
	}
	now := time.Now().UTC()
	var event Event
	m.mu.Lock()
	err = m.database.CallContext(ctx, "create progress job", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err = tx.ExecContext(ctx, `INSERT INTO jobs
			(id, kind, status, stage, progress, site_id, error, created_at, started_at, finished_at, owner_session_id)
			VALUES (?, ?, 'queued', 'queued', 0, ?, NULL, ?, NULL, NULL, ?)`, id.String(), kind, nullableSiteID(siteID), now.Unix(), ownerSessionID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO job_events
			(job_id, kind, stage, status, progress, error_code, created_at)
			VALUES (?, ?, 'queued', 'queued', 0, '', ?)`, id.String(), kind, now.Unix())
		if err != nil {
			return err
		}
		sequence, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		event = Event{Sequence: sequence, JobID: id.String(), Kind: kind, Stage: "queued", Status: "queued", CreatedAt: now, ownerSessionID: ownerSessionID}
		return nil
	})
	if err == nil {
		m.notifyLocked(event)
	}
	m.mu.Unlock()
	if err != nil {
		return "", Event{}, fmt.Errorf("create progress job: %w", err)
	}
	return id.String(), event, nil
}

// Publish appends a durable state transition and then notifies live subscribers.
// ErrorCode is an opaque machine code; free-form errors must not be stored here.
func (m *Manager) Publish(ctx context.Context, jobID string, update Update) (Event, error) {
	if _, err := uuid.Parse(jobID); err != nil || !validLabel(update.Stage) || !validStatus(update.Status) ||
		update.Progress < 0 || update.Progress > 100 || update.ErrorCode != "" && !safeCode.MatchString(update.ErrorCode) {
		return Event{}, errors.New("invalid job update")
	}
	if update.Status == "succeeded" && update.Progress != 100 {
		return Event{}, errors.New("successful job must finish at 100 percent")
	}
	now := time.Now().UTC()
	var event Event
	m.mu.Lock()
	err := m.database.CallContext(ctx, "publish progress job event", func(ctx context.Context, db orm.DB) error {
		beginner, ok := db.(sqlBeginner)
		if !ok {
			return errors.New("database connection does not support transactions")
		}
		tx, err := beginner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var kind, status, ownerSessionID string
		var progress int
		if err = tx.QueryRowContext(ctx, "SELECT kind, status, progress, owner_session_id FROM jobs WHERE id = ?", jobID).Scan(&kind, &status, &progress, &ownerSessionID); err != nil {
			return err
		}
		if terminal(status) || update.Progress < progress || status == "queued" && update.Status == "queued" ||
			update.Status == "queued" || status == "running" && update.Status == "queued" {
			return errors.New("invalid job state transition")
		}
		finishedAt := any(nil)
		if terminal(update.Status) {
			finishedAt = now.Unix()
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, stage = ?, progress = ?, error = ?,
			started_at = CASE WHEN started_at IS NULL AND ? = 'running' THEN ? ELSE started_at END,
			finished_at = ? WHERE id = ?`, update.Status, update.Stage, update.Progress, nullableCode(update.ErrorCode), update.Status, now.Unix(), finishedAt, jobID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO job_events
			(job_id, kind, stage, status, progress, error_code, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			jobID, kind, update.Stage, update.Status, update.Progress, update.ErrorCode, now.Unix())
		if err != nil {
			return err
		}
		sequence, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		event = Event{Sequence: sequence, JobID: jobID, Kind: kind, Stage: update.Stage,
			Status: update.Status, Progress: update.Progress, ErrorCode: update.ErrorCode, CreatedAt: now, ownerSessionID: ownerSessionID}
		return nil
	})
	if err == nil {
		m.notifyLocked(event)
	}
	m.mu.Unlock()
	if err != nil {
		return Event{}, fmt.Errorf("publish progress event: %w", err)
	}
	return event, nil
}

// Subscribe atomically reads history after cursor and registers for live events.
// A reconnect uses Replay.Cursor as the next after sequence.
func (m *Manager) Subscribe(ctx context.Context, ownerSessionID string, after int64, consume func(Event)) (Replay, func(), error) {
	if _, err := uuid.Parse(ownerSessionID); err != nil || after < 0 || consume == nil {
		return Replay{}, nil, errors.New("invalid progress subscription")
	}
	m.mu.Lock()
	replay := Replay{Events: make([]Event, 0, replayLimit), Cursor: after}
	family := make(map[string]struct{})
	err := m.database.CallContext(ctx, "replay progress job events", func(ctx context.Context, db orm.DB) error {
		var err error
		family, err = sessionFamily(ctx, db, ownerSessionID)
		if err != nil {
			return err
		}
		rows, err := db.QueryContext(ctx, `WITH RECURSIVE family(id) AS (
				SELECT id FROM auth_sessions WHERE id = ?
				UNION SELECT s.replaced_by_id FROM auth_sessions s JOIN family f ON s.id = f.id WHERE s.replaced_by_id IS NOT NULL
				UNION SELECT s.id FROM auth_sessions s JOIN family f ON s.replaced_by_id = f.id
			)
			SELECT e.sequence, e.job_id, e.kind, e.stage, e.status, e.progress, e.error_code, e.created_at, j.owner_session_id
			FROM job_events e JOIN jobs j ON j.id = e.job_id
			WHERE e.sequence > ? AND j.owner_session_id IN (SELECT id FROM family) ORDER BY e.sequence LIMIT ?`, ownerSessionID, after, replayLimit+1)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item Event
			var createdAt int64
			if err = rows.Scan(&item.Sequence, &item.JobID, &item.Kind, &item.Stage, &item.Status, &item.Progress, &item.ErrorCode, &createdAt, &item.ownerSessionID); err != nil {
				_ = rows.Close()
				return err
			}
			if len(replay.Events) == replayLimit {
				replay.HasMore = true
				break
			}
			item.CreatedAt = time.Unix(createdAt, 0).UTC()
			replay.Cursor = item.Sequence
			replay.Events = append(replay.Events, item)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		return rows.Close()
	})
	if err != nil {
		m.mu.Unlock()
		return Replay{}, nil, fmt.Errorf("read job event replay: %w", err)
	}
	id := m.nextID.Add(1)
	m.subs[id] = subscriber{id: id, after: replay.Cursor, ownerFamily: family, consume: consume}
	m.mu.Unlock()
	var once sync.Once
	cancel := func() { once.Do(func() { m.mu.Lock(); delete(m.subs, id); m.mu.Unlock() }) }
	return replay, cancel, nil
}

func (m *Manager) notifyLocked(event Event) {
	for _, sub := range m.subs {
		if event.Sequence <= sub.after {
			continue
		}
		if _, allowed := sub.ownerFamily[event.ownerSessionID]; !allowed {
			continue
		}
		sub.consume(event)
		m.subs[sub.id] = subscriber{id: sub.id, after: event.Sequence, ownerFamily: sub.ownerFamily, consume: sub.consume}
	}
}

func sessionFamily(ctx context.Context, db orm.DB, sessionID string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE family(id) AS (
		SELECT id FROM auth_sessions WHERE id = ?
		UNION SELECT s.replaced_by_id FROM auth_sessions s JOIN family f ON s.id = f.id WHERE s.replaced_by_id IS NOT NULL
		UNION SELECT s.id FROM auth_sessions s JOIN family f ON s.replaced_by_id = f.id
	) SELECT id FROM family`, sessionID)
	if err != nil {
		return nil, err
	}
	family := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		family[id] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return family, rows.Close()
}

func validLabel(value string) bool {
	if len(value) == 0 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, ch := range value {
		if ch < 'a' || ch > 'z' {
			if ch < '0' || ch > '9' {
				if ch != '_' && ch != '-' {
					return false
				}
			}
		}
	}
	return true
}

func validStatus(value string) bool {
	switch value {
	case "running", "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}

func nullableCode(code string) any {
	if code == "" {
		return nil
	}
	return code
}

func nullableSiteID(siteID string) any {
	if siteID == "" {
		return nil
	}
	return siteID
}
