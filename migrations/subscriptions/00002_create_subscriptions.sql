-- +goose Up
CREATE TABLE subscriptions (
    id UUID PRIMARY KEY,
    user_id UUID NULL,
    organization_id UUID NULL,
    tariff_id UUID NOT NULL REFERENCES tariffs (id),
    status TEXT NOT NULL,
    message_allowance BIGINT NOT NULL,
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    payment_id TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT subscriptions_status_known CHECK (status IN ('active', 'suspended', 'expired')),
    CONSTRAINT subscriptions_one_owner CHECK (
        (user_id IS NOT NULL AND organization_id IS NULL)
        OR (user_id IS NULL AND organization_id IS NOT NULL)
    ),
    CONSTRAINT subscriptions_period_order CHECK (current_period_end > current_period_start)
);

CREATE UNIQUE INDEX subscriptions_one_active_user
    ON subscriptions (user_id)
    WHERE status = 'active' AND user_id IS NOT NULL;

CREATE UNIQUE INDEX subscriptions_one_active_organization
    ON subscriptions (organization_id)
    WHERE status = 'active' AND organization_id IS NOT NULL;

CREATE INDEX subscriptions_due
    ON subscriptions (current_period_end)
    WHERE status = 'active';

-- +goose Down
DROP TABLE subscriptions;
