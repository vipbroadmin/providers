-- +goose Up
ALTER TABLE sync_runs
    ADD COLUMN IF NOT EXISTS sync_type TEXT NULL,
    ADD COLUMN IF NOT EXISTS items_count INTEGER NULL;

UPDATE sync_runs
SET sync_type = key
WHERE sync_type IS NULL;

ALTER TABLE sync_runs
    ALTER COLUMN sync_type SET NOT NULL;

-- +goose Down
ALTER TABLE sync_runs
    DROP COLUMN IF EXISTS items_count,
    DROP COLUMN IF EXISTS sync_type;
