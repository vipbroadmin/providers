-- +goose Up
CREATE TABLE IF NOT EXISTS sync_runs (
    id BIGSERIAL PRIMARY KEY,
    key TEXT NOT NULL,
    run_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL,
    last_cursor TEXT NULL,
    error TEXT NULL
);

CREATE INDEX IF NOT EXISTS sync_runs_key_started_idx ON sync_runs (key, started_at DESC);
CREATE INDEX IF NOT EXISTS sync_runs_run_id_idx ON sync_runs (run_id);

-- +goose Down
DROP INDEX IF EXISTS sync_runs_run_id_idx;
DROP INDEX IF EXISTS sync_runs_key_started_idx;
DROP TABLE IF EXISTS sync_runs;
