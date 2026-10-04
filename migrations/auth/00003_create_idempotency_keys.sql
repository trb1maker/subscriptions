-- +goose Up
CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    resource_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE idempotency_keys;
