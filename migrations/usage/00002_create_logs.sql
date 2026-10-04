-- +goose Up
CREATE TABLE IF NOT EXISTS usage.logs (
    timestamp DateTime64(3, 'UTC'),
    service LowCardinality(String),
    level LowCardinality(String),
    message String,
    trace_id String,
    span_id String,
    request_id String,
    attrs String
)
ENGINE = MergeTree
ORDER BY (service, timestamp);

-- +goose Down
DROP TABLE IF EXISTS usage.logs;
