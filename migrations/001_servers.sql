CREATE TABLE IF NOT EXISTS "servers"
(
    "id"         INTEGER PRIMARY KEY AUTOINCREMENT,
    "name"       TEXT    NOT NULL,
    "enable"     INTEGER NOT NULL,
    "created_at" TEXT    NOT NULL,
    "updated_at" TEXT    NOT NULL
);