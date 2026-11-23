CREATE TABLE settings (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE sites (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    app_type TEXT NOT NULL CHECK (app_type IN ('spa', 'mpa', 'mda', 'rpa')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE site_domains (
    domain TEXT NOT NULL PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    created_at INTEGER NOT NULL
);

CREATE INDEX site_domains_site_id_idx ON site_domains(site_id);

CREATE TABLE site_config_snapshots (
    id TEXT NOT NULL PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision > 0),
    config_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (site_id, revision)
);

CREATE TABLE releases (
    id TEXT NOT NULL PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id) ON DELETE RESTRICT,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('upload', 'url', 'config')),
    archive_name TEXT NOT NULL,
    content_path TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('staged', 'ready', 'failed')),
    created_at INTEGER NOT NULL
);

CREATE INDEX releases_site_created_idx ON releases(site_id, created_at DESC);

CREATE TABLE site_activations (
    site_id TEXT NOT NULL PRIMARY KEY REFERENCES sites(id) ON DELETE RESTRICT,
    release_id TEXT NOT NULL REFERENCES releases(id) ON DELETE RESTRICT,
    config_snapshot_id TEXT NOT NULL REFERENCES site_config_snapshots(id) ON DELETE RESTRICT,
    activated_at INTEGER NOT NULL
);

CREATE TABLE registry_sources (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    url TEXT NOT NULL,
    url_query_ciphertext BLOB,
    auth_type TEXT NOT NULL CHECK (auth_type IN ('none', 'basic', 'bearer', 'query')),
    credentials_ciphertext BLOB,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE owners (
    id INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    totp_secret_ciphertext BLOB,
    totp_confirmed INTEGER NOT NULL DEFAULT 0 CHECK (totp_confirmed IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE owner_recovery_codes (
    code_hash TEXT NOT NULL PRIMARY KEY,
    created_at INTEGER NOT NULL,
    used_at INTEGER
);

CREATE TABLE auth_sessions (
    id TEXT NOT NULL PRIMARY KEY,
    refresh_token_hash BLOB NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    replaced_by_id TEXT REFERENCES auth_sessions(id) ON DELETE RESTRICT
);

CREATE TABLE jobs (
    id TEXT NOT NULL PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    stage TEXT NOT NULL DEFAULT '',
    progress INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    site_id TEXT REFERENCES sites(id) ON DELETE RESTRICT,
    error TEXT,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    finished_at INTEGER
);

CREATE INDEX jobs_status_created_idx ON jobs(status, created_at DESC);

CREATE TABLE certificates (
    id TEXT NOT NULL PRIMARY KEY,
    domain TEXT NOT NULL UNIQUE,
    issuer TEXT NOT NULL CHECK (issuer IN ('self_signed', 'letsencrypt')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'valid', 'expiring', 'failed')),
    certificate_path TEXT NOT NULL,
    private_key_path TEXT NOT NULL,
    not_before INTEGER,
    not_after INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
