package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	TelegramID  int64
	Username    string
	Enabled     bool
	ConfigLimit int
}

type Peer struct {
	ID         int64
	TelegramID int64
	ServerID   string
	PeerID     string // публичный ключ клиента; приватный нигде не хранится
	DeviceName string
	ClientIP   string
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

type StatusMessage struct {
	ServerID  string
	AdminID   int64
	ChatID    int64
	MessageID int64
}

// KnownUser — пользователь, который хоть раз писал боту; источник для
// резолва @username → Telegram ID в админ-сценариях.
type KnownUser struct {
	TelegramID int64
	Username   string
	FirstName  string
}

// Invite — заранее выданное приглашение по @username; активируется,
// когда человек впервые пишет боту.
type Invite struct {
	Username    string
	ConfigLimit int
}

type Store interface {
	UpsertUser(ctx context.Context, u User) error
	GetUser(ctx context.Context, telegramID int64) (User, error)
	SetUserEnabled(ctx context.Context, telegramID int64, enabled bool) error
	SetUserLimit(ctx context.Context, telegramID int64, limit int) error
	SetUsername(ctx context.Context, telegramID int64, username string) error
	ListUsers(ctx context.Context) ([]User, error)

	GrantAccess(ctx context.Context, telegramID int64, serverID string) error
	HasAccess(ctx context.Context, telegramID int64, serverID string) (bool, error)
	ListUserServers(ctx context.Context, telegramID int64) ([]string, error)

	CreatePeer(ctx context.Context, p Peer) (Peer, error)
	ListActivePeers(ctx context.Context, telegramID int64) ([]Peer, error)
	ListActivePeersOnServer(ctx context.Context, serverID string) ([]Peer, error)
	ListActivePeersAll(ctx context.Context) ([]Peer, error)
	GetActivePeer(ctx context.Context, telegramID int64, id int64) (Peer, error)
	RevokePeer(ctx context.Context, id int64) error

	GetStatusMessage(ctx context.Context, serverID string, adminID int64) (StatusMessage, error)
	SaveStatusMessage(ctx context.Context, m StatusMessage) error

	UpsertKnownUser(ctx context.Context, telegramID int64, username, firstName string) error
	FindKnownUser(ctx context.Context, username string) (KnownUser, error)

	CreateInvite(ctx context.Context, username string, configLimit int) error
	TakeInvite(ctx context.Context, username string) (Invite, error)
	ListInvites(ctx context.Context) ([]Invite, error)
}
