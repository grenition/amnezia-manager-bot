-- +goose Up
CREATE TABLE known_users (
    telegram_id BIGINT PRIMARY KEY,
    username    TEXT NOT NULL,
    first_name  TEXT NOT NULL DEFAULT '',
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_known_users_username ON known_users (username);

-- +goose Down
DROP TABLE known_users;
