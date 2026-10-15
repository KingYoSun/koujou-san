CREATE TABLE auth_users (
    id text PRIMARY KEY,
    issuer text NOT NULL,
    subject text NOT NULL,
    email text NOT NULL,
    display_name text NOT NULL,
    administrator boolean NOT NULL DEFAULT false,
    UNIQUE(issuer, subject)
);
CREATE TABLE auth_attempts (
    state text PRIMARY KEY,
    verifier bytea NOT NULL,
    nonce text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE TABLE auth_sessions (
    id text PRIMARY KEY,
    tokens bytea NOT NULL,
    expires_at timestamptz NOT NULL
);
