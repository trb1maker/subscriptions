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

-- name: InsertOrganization :exec
INSERT INTO organizations (id, name)
VALUES ($1, $2);

-- name: FindOrganization :one
SELECT id, name
FROM organizations
WHERE id = $1;

-- name: InsertUser :exec
INSERT INTO users (id, email, password_hash, organization_id, role)
VALUES ($1, $2, $3, $4, $5);

-- name: FindUserByEmail :one
SELECT id, email, password_hash, organization_id, role
FROM users
WHERE email = $1;

-- name: FindUserByID :one
SELECT id, email, password_hash, organization_id, role
FROM users
WHERE id = $1;
