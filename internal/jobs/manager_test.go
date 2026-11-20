package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"go.arwos.org/jasta/internal/storage"
)

func TestJobEventsReplayLiveUpdatesAndTerminalState(t *testing.T) {
	ctx := context.Background()
	database := orm.New(ctx)
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{
		Tags: "master", File: filepath.Join(t.TempDir(), "jobs.sqlite"), Cache: "private", Mode: "rwc",
		Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_foreign_keys=on",
	}}}); err != nil {
		t.Fatalf("ApplyConfig(): %v", err)
	}
	t.Cleanup(database.Close)
	if err := storage.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	manager, err := New(database.Tag("master"))
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	sessionID := uuid.NewString()
	if err = insertJobTestSession(ctx, database.Tag("master"), sessionID); err != nil {
		t.Fatalf("insert owner session: %v", err)
	}
	foreignSessionID := uuid.NewString()
	if err = insertJobTestSession(ctx, database.Tag("master"), foreignSessionID); err != nil {
		t.Fatalf("insert foreign owner session: %v", err)
	}
	jobID, initial, err := manager.Create(ctx, "deploy", "", sessionID)
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if initial.Status != "queued" || initial.Sequence <= 0 {
		t.Fatalf("initial event=%#v", initial)
	}
	seen := make(chan Event, 4)
	replay, cancel, err := manager.Subscribe(ctx, sessionID, 0, func(event Event) { seen <- event })
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	defer cancel()
	if len(replay.Events) != 1 || replay.Events[0].JobID != jobID || replay.Cursor != initial.Sequence {
		t.Fatalf("initial replay=%#v", replay)
	}
	foreignReplay, foreignCancel, err := manager.Subscribe(ctx, foreignSessionID, 0, func(event Event) { seen <- event })
	if err != nil {
		t.Fatalf("foreign Subscribe(): %v", err)
	}
	defer foreignCancel()
	if len(foreignReplay.Events) != 0 {
		t.Fatalf("foreign session received another session's job events: %#v", foreignReplay.Events)
	}
	for _, update := range []Update{
		{Stage: "download", Status: "running", Progress: 10},
		{Stage: "build", Status: "running", Progress: 65},
		{Stage: "publish", Status: "succeeded", Progress: 100},
	} {
		if _, err = manager.Publish(ctx, jobID, update); err != nil {
			t.Fatalf("Publish(%s): %v", update.Stage, err)
		}
	}
	for want := int64(2); want <= 4; want++ {
		select {
		case event := <-seen:
			if event.Sequence != want {
				t.Fatalf("live event sequence=%d, want %d", event.Sequence, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", want)
		}
	}
	select {
	case event := <-seen:
		t.Fatalf("foreign session received live job event: %#v", event)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err = manager.Publish(ctx, jobID, Update{Stage: "retry", Status: "running", Progress: 100}); err == nil {
		t.Fatal("Publish() changed terminal job")
	}
	rotatedSessionID := uuid.NewString()
	if err = insertJobTestSession(ctx, database.Tag("master"), rotatedSessionID); err != nil {
		t.Fatalf("insert rotated owner session: %v", err)
	}
	if err = database.Tag("master").CallContext(ctx, "link rotated progress session", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at = 2, replaced_by_id = ? WHERE id = ?", rotatedSessionID, sessionID)
		return err
	}); err != nil {
		t.Fatalf("link rotated owner session: %v", err)
	}
	reconnected, _, err := manager.Subscribe(ctx, rotatedSessionID, initial.Sequence, func(Event) {})
	if err != nil {
		t.Fatalf("Subscribe() after reconnect: %v", err)
	}
	if len(reconnected.Events) != 3 || reconnected.Events[2].Status != "succeeded" || reconnected.Cursor != 4 {
		t.Fatalf("reconnected replay=%#v", reconnected)
	}
}

func insertJobTestSession(ctx context.Context, database orm.Stmt, sessionID string) error {
	return database.CallContext(ctx, "insert test session for progress job", func(ctx context.Context, db orm.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO auth_sessions
			(id, refresh_token_hash, created_at, last_used_at, expires_at, revoked_at, replaced_by_id)
			VALUES (?, ?, 1, 1, ?, NULL, NULL)`, sessionID, []byte(sessionID), time.Now().Add(time.Hour).Unix())
		return err
	})
}
