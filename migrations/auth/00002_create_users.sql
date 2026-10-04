-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    organization_id UUID NULL REFERENCES organizations (id),
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email),
    CONSTRAINT users_role_known CHECK (role IN ('user', 'admin'))
);

-- +goose Down
DROP TABLE users;
