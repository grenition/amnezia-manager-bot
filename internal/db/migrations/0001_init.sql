-- +goose Up
CREATE TABLE users (
    telegram_id  BIGINT PRIMARY KEY,
    username     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    config_limit INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_server_access (
    telegram_id BIGINT NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    server_id   TEXT NOT NULL,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (telegram_id, server_id)
);

CREATE TABLE vpn_peers (
    id          BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL REFERENCES users (telegram_id),
    server_id   TEXT NOT NULL,
    peer_id     TEXT NOT NULL,
    device_name TEXT NOT NULL,
    client_ip   INET NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_vpn_peers_active ON vpn_peers (telegram_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX uq_vpn_peers_active_pub ON vpn_peers (server_id, peer_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX uq_vpn_peers_active_ip ON vpn_peers (server_id, client_ip) WHERE revoked_at IS NULL;

CREATE TABLE server_status_messages (
    server_id  TEXT NOT NULL,
    admin_id   BIGINT NOT NULL,
    chat_id    BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, admin_id)
);

-- +goose Down
DROP TABLE server_status_messages;
DROP TABLE vpn_peers;
DROP TABLE user_server_access;
DROP TABLE users;
