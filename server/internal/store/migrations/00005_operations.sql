-- Job runs and owner authentication. The auth tables store hashes, never
-- the secrets.

-- +goose Up
CREATE TABLE ingest_runs (
    id          bigserial PRIMARY KEY,
    job         text NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status      text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    stats       jsonb NOT NULL DEFAULT '{}',
    error       text NOT NULL DEFAULT '',
    CHECK ((status = 'running') = (finished_at IS NULL))
);

CREATE INDEX ingest_runs_job_started ON ingest_runs (job, started_at DESC, id DESC);

CREATE TABLE sessions (
    token_hash   bytea PRIMARY KEY,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    user_agent   text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT ''
);

CREATE INDEX sessions_expires_at ON sessions (expires_at);

CREATE TABLE api_tokens (
    id           bigserial PRIMARY KEY,
    name         text NOT NULL UNIQUE,
    token_hash   bytea NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

-- +goose Down
DROP TABLE api_tokens;
DROP TABLE sessions;
DROP TABLE ingest_runs;
