package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"amnezia-manager-bot/internal/store"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (store.User, error) {
	var u store.User
	err := r.Scan(&u.TelegramID, &u.Username, &u.Enabled, &u.ConfigLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, store.ErrNotFound
	}
	return u, err
}

const peerCols = "id, telegram_id, server_id, peer_id, device_name, host(client_ip), created_at, revoked_at"

func scanPeer(r scanner) (store.Peer, error) {
	var p store.Peer
	err := r.Scan(&p.ID, &p.TelegramID, &p.ServerID, &p.PeerID, &p.DeviceName, &p.ClientIP, &p.CreatedAt, &p.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, store.ErrNotFound
	}
	return p, err
}

func (s *Store) UpsertUser(ctx context.Context, u store.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (telegram_id, username, enabled, config_limit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE
		SET username = EXCLUDED.username, enabled = EXCLUDED.enabled, config_limit = EXCLUDED.config_limit`,
		u.TelegramID, u.Username, u.Enabled, u.ConfigLimit)
	return err
}

func (s *Store) GetUser(ctx context.Context, telegramID int64) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		"SELECT telegram_id, username, enabled, config_limit FROM users WHERE telegram_id = $1", telegramID))
}

func (s *Store) SetUserEnabled(ctx context.Context, telegramID int64, enabled bool) error {
	return s.execUserUpdate(ctx, "UPDATE users SET enabled = $2 WHERE telegram_id = $1", telegramID, enabled)
}

func (s *Store) SetUserLimit(ctx context.Context, telegramID int64, limit int) error {
	return s.execUserUpdate(ctx, "UPDATE users SET config_limit = $2 WHERE telegram_id = $1", telegramID, limit)
}

func (s *Store) SetUsername(ctx context.Context, telegramID int64, username string) error {
	return s.execUserUpdate(ctx, "UPDATE users SET username = $2 WHERE telegram_id = $1", telegramID, username)
}

func (s *Store) execUserUpdate(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, "SELECT telegram_id, username, enabled, config_limit FROM users ORDER BY telegram_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GrantAccess(ctx context.Context, telegramID int64, serverID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_server_access (telegram_id, server_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, telegramID, serverID)
	return err
}

func (s *Store) HasAccess(ctx context.Context, telegramID int64, serverID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM user_server_access WHERE telegram_id = $1 AND server_id = $2)",
		telegramID, serverID).Scan(&ok)
	return ok, err
}

func (s *Store) ListUserServers(ctx context.Context, telegramID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT server_id FROM user_server_access WHERE telegram_id = $1 ORDER BY server_id", telegramID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) CreatePeer(ctx context.Context, p store.Peer) (store.Peer, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vpn_peers (telegram_id, server_id, peer_id, device_name, client_ip)
		VALUES ($1, $2, $3, $4, $5::inet)
		RETURNING id, created_at`,
		p.TelegramID, p.ServerID, p.PeerID, p.DeviceName, p.ClientIP).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return p, fmt.Errorf("insert peer: %w", err)
	}
	return p, nil
}

func (s *Store) listPeers(ctx context.Context, sql string, args ...any) ([]store.Peer, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Peer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListActivePeers(ctx context.Context, telegramID int64) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE telegram_id = $1 AND revoked_at IS NULL ORDER BY id", telegramID)
}

func (s *Store) ListActivePeersOnServer(ctx context.Context, serverID string) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE server_id = $1 AND revoked_at IS NULL ORDER BY id", serverID)
}

func (s *Store) ListActivePeersAll(ctx context.Context) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE revoked_at IS NULL ORDER BY id")
}

func (s *Store) GetActivePeer(ctx context.Context, telegramID int64, id int64) (store.Peer, error) {
	return scanPeer(s.pool.QueryRow(ctx,
		"SELECT "+peerCols+" FROM vpn_peers WHERE id = $1 AND telegram_id = $2 AND revoked_at IS NULL", id, telegramID))
}

func (s *Store) RevokePeer(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, "UPDATE vpn_peers SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetStatusMessage(ctx context.Context, serverID string, adminID int64) (store.StatusMessage, error) {
	var sm store.StatusMessage
	err := s.pool.QueryRow(ctx,
		"SELECT server_id, admin_id, chat_id, message_id FROM server_status_messages WHERE server_id = $1 AND admin_id = $2",
		serverID, adminID).Scan(&sm.ServerID, &sm.AdminID, &sm.ChatID, &sm.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sm, store.ErrNotFound
	}
	return sm, err
}

func (s *Store) SaveStatusMessage(ctx context.Context, sm store.StatusMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO server_status_messages (server_id, admin_id, chat_id, message_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id, admin_id) DO UPDATE
		SET chat_id = EXCLUDED.chat_id, message_id = EXCLUDED.message_id, updated_at = now()`,
		sm.ServerID, sm.AdminID, sm.ChatID, sm.MessageID)
	return err
}

func (s *Store) UpsertKnownUser(ctx context.Context, telegramID int64, username, firstName string) error {
	if _, err := s.pool.Exec(ctx,
		"UPDATE known_users SET username = telegram_id::text WHERE username = $1 AND telegram_id <> $2",
		username, telegramID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO known_users (telegram_id, username, first_name, last_seen)
		VALUES ($2, $1, $3, now())
		ON CONFLICT (telegram_id) DO UPDATE
		SET username = EXCLUDED.username, first_name = EXCLUDED.first_name, last_seen = now()`,
		username, telegramID, firstName)
	return err
}

func (s *Store) FindKnownUser(ctx context.Context, username string) (store.KnownUser, error) {
	var u store.KnownUser
	err := s.pool.QueryRow(ctx,
		"SELECT telegram_id, username, first_name FROM known_users WHERE username = $1", username).
		Scan(&u.TelegramID, &u.Username, &u.FirstName)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, store.ErrNotFound
	}
	return u, err
}
