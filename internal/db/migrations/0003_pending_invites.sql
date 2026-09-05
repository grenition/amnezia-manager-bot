-- +goose Up
CREATE TABLE pending_invites (
    username     TEXT PRIMARY KEY,
    config_limit INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE pending_invites;
