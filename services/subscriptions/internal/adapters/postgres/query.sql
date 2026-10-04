-- name: FindIdempotencyForUpdate :one
SELECT key, operation, request_hash, resource_id
FROM idempotency_keys
WHERE key = $1
FOR UPDATE;

-- name: FindIdempotency :one
SELECT key, operation, request_hash, resource_id
FROM idempotency_keys
WHERE key = $1;

-- name: InsertIdempotency :exec
INSERT INTO idempotency_keys (key, operation, request_hash, resource_id)
VALUES ($1, $2, $3, $4);

-- name: InsertTariff :exec
INSERT INTO tariffs (id, name, monthly_price_minor, message_limit, type, is_base_tariff)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: FindTariff :one
SELECT id, name, monthly_price_minor, message_limit, type, is_base_tariff
FROM tariffs
WHERE id = $1;

-- name: ListTariffs :many
SELECT id, name, monthly_price_minor, message_limit, type, is_base_tariff
FROM tariffs
ORDER BY created_at, id;

-- name: FindBaseTariff :one
SELECT id, name, monthly_price_minor, message_limit, type, is_base_tariff
FROM tariffs
WHERE is_base_tariff;

-- name: InsertSubscription :exec
INSERT INTO subscriptions (
    id,
    user_id,
    organization_id,
    tariff_id,
    status,
    message_allowance,
    current_period_start,
    current_period_end
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: FindSubscription :one
SELECT
    id,
    user_id,
    organization_id,
    tariff_id,
    status,
    message_allowance,
    current_period_start,
    current_period_end
FROM subscriptions
WHERE id = $1;

-- name: LockSubscription :one
SELECT
    id,
    user_id,
    organization_id,
    tariff_id,
    status,
    message_allowance,
    current_period_start,
    current_period_end
FROM subscriptions
WHERE id = $1
FOR UPDATE;

-- name: FindActiveSubscriptionByUser :one
SELECT
    id,
    user_id,
    organization_id,
    tariff_id,
    status,
    message_allowance,
    current_period_start,
    current_period_end
FROM subscriptions
WHERE user_id = $1 AND status = 'active';

-- name: FindActiveSubscriptionByOrganization :one
SELECT
    id,
    user_id,
    organization_id,
    tariff_id,
    status,
    message_allowance,
    current_period_start,
    current_period_end
FROM subscriptions
WHERE organization_id = $1 AND status = 'active';

-- name: UpdateSubscription :exec
UPDATE subscriptions
SET
    tariff_id = $2,
    status = $3,
    message_allowance = $4,
    current_period_start = $5,
    current_period_end = $6,
    updated_at = now()
WHERE id = $1;

-- name: LockDueSubscriptions :many
SELECT
    subscriptions.id,
    subscriptions.user_id,
    subscriptions.organization_id,
    subscriptions.tariff_id,
    subscriptions.status,
    subscriptions.message_allowance,
    subscriptions.current_period_start,
    subscriptions.current_period_end,
    tariffs.name,
    tariffs.monthly_price_minor,
    tariffs.message_limit,
    tariffs.type,
    tariffs.is_base_tariff
FROM subscriptions
JOIN tariffs ON tariffs.id = subscriptions.tariff_id
WHERE subscriptions.status = 'active'
  AND subscriptions.current_period_end <= $1
ORDER BY subscriptions.id
FOR UPDATE OF subscriptions SKIP LOCKED;
