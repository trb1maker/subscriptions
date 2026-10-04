-- +goose Up
CREATE DATABASE IF NOT EXISTS usage;

CREATE TABLE IF NOT EXISTS usage.events (
    event_id UUID,
    event_type LowCardinality(String),
    occurred_at DateTime64(3, 'UTC'),
    owner_kind LowCardinality(String),
    owner_id UUID,
    allowance Nullable(Int64),
    tokens Int64,
    payment_id String,
    amount_minor Int64
)
ENGINE = ReplacingMergeTree
ORDER BY (owner_kind, owner_id, event_id);

CREATE TABLE IF NOT EXISTS usage.payments (
    event_id UUID,
    occurred_at DateTime64(3, 'UTC'),
    owner_kind LowCardinality(String),
    owner_id UUID,
    payment_id String,
    amount_minor Int64
)
ENGINE = ReplacingMergeTree
ORDER BY (event_id);

CREATE TABLE IF NOT EXISTS usage.token_usage (
    event_id UUID,
    occurred_at DateTime64(3, 'UTC'),
    owner_kind LowCardinality(String),
    owner_id UUID,
    tokens Int64,
    outcome LowCardinality(String)
)
ENGINE = ReplacingMergeTree
ORDER BY (event_id);

-- +goose Down
DROP TABLE IF EXISTS usage.token_usage;
DROP TABLE IF EXISTS usage.payments;
DROP TABLE IF EXISTS usage.events;
DROP DATABASE IF EXISTS usage;
