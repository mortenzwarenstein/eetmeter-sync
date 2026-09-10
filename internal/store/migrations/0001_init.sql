-- 0001_init: sync bookkeeping tables.
-- The migration runner strips line comments, then splits on the semicolon.
-- Keep every statement semicolon-terminated and use no semicolons elsewhere.

CREATE TABLE account (
    id         text PRIMARY KEY,
    label      text NOT NULL,
    email      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recipe_link (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                 text NOT NULL UNIQUE,
    a_recipe_id          text,
    b_recipe_id          text,
    a_baseline_hash      bytea,
    b_baseline_hash      bytea,
    status               text NOT NULL DEFAULT 'active',
    last_synced_at       timestamptz,
    conflict_detected_at timestamptz,
    note                 text NOT NULL DEFAULT '',
    a_conflict_snapshot  jsonb,
    b_conflict_snapshot  jsonb,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT recipe_link_status_chk CHECK (status IN ('active', 'conflict', 'retired'))
);

CREATE TABLE sync_run (
    id          text PRIMARY KEY,
    trigger     text NOT NULL,
    started_at  timestamptz NOT NULL,
    finished_at timestamptz,
    status      text NOT NULL,
    error       text,
    summary     jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT sync_run_trigger_chk CHECK (trigger IN ('scheduled', 'manual')),
    CONSTRAINT sync_run_status_chk CHECK (status IN ('running', 'succeeded', 'failed'))
);

CREATE UNIQUE INDEX one_running_sync ON sync_run ((status)) WHERE status = 'running';

CREATE TABLE conflict_resolution (
    link_id      bigint PRIMARY KEY REFERENCES recipe_link (id) ON DELETE CASCADE,
    winner       text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conflict_resolution_winner_chk CHECK (winner IN ('a', 'b'))
);
