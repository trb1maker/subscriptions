-- +goose Up
CREATE TABLE tariffs (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    monthly_price_minor BIGINT NOT NULL,
    message_limit INTEGER NOT NULL,
    type TEXT NOT NULL,
    is_base_tariff BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tariffs_type_known CHECK (type IN ('b2c', 'b2b')),
    CONSTRAINT tariffs_price_non_negative CHECK (monthly_price_minor >= 0),
    CONSTRAINT tariffs_limit_positive CHECK (message_limit > 0),
    CONSTRAINT tariffs_base_is_b2c CHECK (NOT is_base_tariff OR type = 'b2c')
);

CREATE UNIQUE INDEX tariffs_one_base ON tariffs ((TRUE)) WHERE is_base_tariff;

-- +goose Down
DROP TABLE tariffs;
