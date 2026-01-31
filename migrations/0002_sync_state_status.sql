-- +goose Up
ALTER TABLE sync_state
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'idle',
    ADD COLUMN IF NOT EXISTS current_run_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS current_started_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS last_error TEXT NULL;

-- +goose Down
ALTER TABLE sync_state
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_success_at,
    DROP COLUMN IF EXISTS current_started_at,
    DROP COLUMN IF EXISTS current_run_id,
    DROP COLUMN IF EXISTS status;
