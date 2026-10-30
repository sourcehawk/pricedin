-- Each session records which owner credentials started it, so that a new
-- password or owner email ends the sessions of the old one.

-- +goose Up
ALTER TABLE sessions ADD COLUMN owner_fingerprint bytea NOT NULL DEFAULT ''::bytea;

-- +goose Down
ALTER TABLE sessions DROP COLUMN owner_fingerprint;
