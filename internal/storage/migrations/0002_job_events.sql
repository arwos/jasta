ALTER TABLE jobs ADD COLUMN owner_session_id TEXT REFERENCES auth_sessions(id) ON DELETE SET NULL;

CREATE TABLE job_events (
    sequence INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    stage TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    progress INTEGER NOT NULL CHECK (progress BETWEEN 0 AND 100),
    error_code TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX job_events_job_sequence_idx ON job_events(job_id, sequence);
