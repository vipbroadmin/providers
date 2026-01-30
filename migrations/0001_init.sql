-- +goose Up
CREATE TABLE IF NOT EXISTS providers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    label TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    source TEXT NOT NULL DEFAULT 'slotegrator',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS games (
    game_uuid TEXT PRIMARY KEY,
    provider_id INTEGER NOT NULL REFERENCES providers(id),
    name TEXT NOT NULL,
    type TEXT NULL,
    provider_name TEXT NULL,
    technology TEXT NULL,
    has_lobby BOOLEAN NOT NULL DEFAULT false,
    is_mobile BOOLEAN NOT NULL DEFAULT false,
    has_freespins BOOLEAN NOT NULL DEFAULT false,
    has_tables BOOLEAN NOT NULL DEFAULT false,
    label TEXT NULL,
    image TEXT NULL,
    rtp NUMERIC(6,3) NULL,
    volatility TEXT NULL,
    tags JSONB NULL,
    parameters JSONB NULL,
    images JSONB NULL,
    related_games JSONB NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS games_provider_idx ON games (provider_id);
CREATE INDEX IF NOT EXISTS games_status_idx ON games (status);
CREATE INDEX IF NOT EXISTS games_name_idx ON games (name);

CREATE TABLE IF NOT EXISTS game_sessions (
    session_id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL,
    provider_id INTEGER NULL REFERENCES providers(id),
    game_uuid TEXT NOT NULL REFERENCES games(game_uuid),
    currency TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    launch_url TEXT NULL,
    device TEXT NULL,
    return_url TEXT NULL,
    language TEXT NULL,
    last_round_id TEXT NULL,
    last_transaction_id TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS game_sessions_player_idx ON game_sessions (player_id);
CREATE INDEX IF NOT EXISTS game_sessions_provider_idx ON game_sessions (provider_id);
CREATE INDEX IF NOT EXISTS game_sessions_game_idx ON game_sessions (game_uuid);
CREATE INDEX IF NOT EXISTS game_sessions_status_idx ON game_sessions (status);

CREATE TABLE IF NOT EXISTS sync_state (
    key TEXT PRIMARY KEY,
    last_sync_at TIMESTAMPTZ NULL,
    last_cursor TEXT NULL
);

-- +goose Down
DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS game_sessions;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS providers;
