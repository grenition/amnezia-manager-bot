package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/netalloc"
	"amnezia-manager-bot/internal/patcher"
	"amnezia-manager-bot/internal/store"
	"amnezia-manager-bot/internal/vpn"
)

var (
	ErrNoAccess      = errors.New("no access")
	ErrLimitReached  = errors.New("config limit reached")
	ErrNotFound      = errors.New("not found")
	ErrBadDeviceName = errors.New("invalid device name")
	ErrBadLimit      = errors.New("invalid limit")
	ErrBadUsername   = errors.New("invalid username")
)

// IPListSource — источник списка AllowedIPs (реализация — routes.Service).
type IPListSource interface {
	AllowedIPs() []string
}

var deviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,30}[a-zA-Z0-9]$`)

type CreatedConfig struct {
	FileName   string
	Content    string
	DeviceName string
}

type Service struct {
	cfg    config.Config
	store  store.Store
	vpn    vpn.Provider
	ips    IPListSource
	log    *slog.Logger
	admins map[int64]struct{}
}

func New(cfg config.Config, st store.Store, vp vpn.Provider, ips IPListSource, log *slog.Logger) *Service {
	admins := make(map[int64]struct{}, len(cfg.AdminIDs))
	for _, id := range cfg.AdminIDs {
		admins[id] = struct{}{}
	}
	return &Service{cfg: cfg, store: st, vpn: vp, ips: ips, log: log, admins: admins}
}

func (s *Service) IsAdmin(id int64) bool {
	_, ok := s.admins[id]
	return ok
}

func (s *Service) user(ctx context.Context, telegramID int64) (store.User, error) {
	u, err := s.store.GetUser(ctx, telegramID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return u, ErrNoAccess
	case err != nil:
		return u, err
	case !u.Enabled:
		return u, ErrNoAccess
	}
	return u, nil
}

func (s *Service) CheckAccess(ctx context.Context, telegramID int64) error {
	_, err := s.user(ctx, telegramID)
	return err
}

// CreateConfig: доступ и лимит → peer на сервере → патч AllowedIPs → метаданные в БД.
// Полный конфиг и приватный ключ нигде не сохраняются.
func (s *Service) CreateConfig(ctx context.Context, telegramID int64, deviceName string) (CreatedConfig, error) {
	if !deviceNameRe.MatchString(deviceName) {
		return CreatedConfig{}, ErrBadDeviceName
	}
	u, err := s.user(ctx, telegramID)
	if err != nil {
		return CreatedConfig{}, err
	}
	srv, err := s.cfg.DefaultServer()
	if err != nil {
		return CreatedConfig{}, err
	}
	ok, err := s.store.HasAccess(ctx, telegramID, srv.ID)
	if err != nil {
		return CreatedConfig{}, err
	}
	if !ok {
		return CreatedConfig{}, ErrNoAccess
	}
	active, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return CreatedConfig{}, err
	}
	if len(active) >= u.ConfigLimit {
		return CreatedConfig{}, ErrLimitReached
	}
	onServer, err := s.store.ListActivePeersOnServer(ctx, srv.ID)
	if err != nil {
		return CreatedConfig{}, err
	}
	_, cidr, err := net.ParseCIDR(srv.ClientCIDR)
	if err != nil {
		return CreatedConfig{}, fmt.Errorf("bad client_cidr: %w", err)
	}
	used := make([]net.IP, 0, len(onServer))
	for _, p := range onServer {
		used = append(used, net.ParseIP(p.ClientIP))
	}
	ip, err := netalloc.Allocate(cidr, used)
	if err != nil {
		return CreatedConfig{}, err
	}
	priv, pub, err := vpn.GenerateKeyPair()
	if err != nil {
		return CreatedConfig{}, err
	}
	if err := s.vpn.CreatePeer(ctx, srv.ID, pub, ip.String()); err != nil {
		return CreatedConfig{}, fmt.Errorf("create peer on server: %w", err)
	}
	patched, err := patcher.Patch(vpn.BuildClientConfig(srv, priv, ip.String()), s.ips.AllowedIPs())
	if err != nil {
		s.bestEffortRemove(ctx, srv.ID, pub)
		return CreatedConfig{}, err
	}
	p, err := s.store.CreatePeer(ctx, store.Peer{
		TelegramID: telegramID, ServerID: srv.ID, PeerID: pub,
		DeviceName: deviceName, ClientIP: ip.String(),
	})
	if err != nil {
		s.bestEffortRemove(ctx, srv.ID, pub)
		return CreatedConfig{}, fmt.Errorf("save peer: %w", err)
	}
	s.log.Info("config created", "user", telegramID, "peer_db_id", p.ID, "server", srv.ID, "device", deviceName)
	return CreatedConfig{FileName: deviceName + ".conf", Content: patched, DeviceName: deviceName}, nil
}

func (s *Service) bestEffortRemove(ctx context.Context, serverID, pub string) {
	if err := s.vpn.RemovePeer(ctx, serverID, pub); err != nil {
		s.log.Error("cleanup failed, peer left on server", "server", serverID)
	}
}

func (s *Service) ListDevices(ctx context.Context, telegramID int64) ([]store.Peer, int, error) {
	u, err := s.user(ctx, telegramID)
	if err != nil {
		return nil, 0, err
	}
	peers, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return nil, 0, err
	}
	return peers, u.ConfigLimit, nil
}

func (s *Service) DeleteConfig(ctx context.Context, telegramID int64, peerDBID int64) error {
	p, err := s.store.GetActivePeer(ctx, telegramID, peerDBID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.vpn.RemovePeer(ctx, p.ServerID, p.PeerID); err != nil {
		return fmt.Errorf("remove peer from server: %w", err)
	}
	if err := s.store.RevokePeer(ctx, p.ID); err != nil {
		return fmt.Errorf("revoke peer: %w", err)
	}
	s.log.Info("config deleted", "user", telegramID, "peer_db_id", p.ID, "server", p.ServerID)
	return nil
}

// ServerForComplaint возвращает сервер для контекста обращения:
// сервер первого конфига пользователя либо первый включённый.
func (s *Service) ServerForComplaint(ctx context.Context, telegramID int64) (string, string, error) {
	peers, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return "", "", err
	}
	id := ""
	if len(peers) > 0 {
		id = peers[0].ServerID
	} else if ss := s.cfg.EnabledServers(); len(ss) > 0 {
		id = ss[0].ID
	}
	srv, ok := s.cfg.ServerByID(id)
	if !ok {
		return "", "", errors.New("no server for complaint")
	}
	return srv.ID, srv.DisplayName, nil
}

// RememberUser обновляет соответствие Telegram ID ↔ @username для всех,
// кто пишет боту, и активирует заранее выданные приглашения.
func (s *Service) RememberUser(ctx context.Context, telegramID int64, username, firstName string) {
	if username == "" {
		return
	}
	username = strings.ToLower(username)
	if err := s.store.UpsertKnownUser(ctx, telegramID, username, firstName); err != nil {
		s.log.Warn("remember user failed", "user", telegramID, "err", err)
		return
	}
	if _, err := s.store.TakeInvite(ctx, username); err == nil {
		if _, err := s.AdminAddUser(ctx, telegramID, username); err != nil {
			s.log.Error("invite redeem failed", "user", telegramID, "err", err)
		} else {
			s.log.Info("invite redeemed", "user", telegramID, "username", username)
		}
	}
}

func (s *Service) FindKnownUser(ctx context.Context, username string) (store.KnownUser, error) {
	username = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
	return s.store.FindKnownUser(ctx, username)
}

// CreateInvite выдаёт приглашение по @username для человека, ещё не писавшего боту.
func (s *Service) CreateInvite(ctx context.Context, username string) error {
	username = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
	if username == "" || strings.ContainsAny(username, " @") {
		return ErrBadUsername
	}
	return s.store.CreateInvite(ctx, username, s.cfg.DefaultLimit)
}

func (s *Service) ListInvites(ctx context.Context) ([]store.Invite, error) {
	return s.store.ListInvites(ctx)
}

// AdminAddUser регистрирует пользователя с лимитом по умолчанию
// и доступом ко всем включённым серверам.
func (s *Service) AdminAddUser(ctx context.Context, telegramID int64, username string) (store.User, error) {
	u := store.User{TelegramID: telegramID, Username: username, Enabled: true, ConfigLimit: s.cfg.DefaultLimit}
	if err := s.store.UpsertUser(ctx, u); err != nil {
		return store.User{}, err
	}
	for _, srv := range s.cfg.EnabledServers() {
		if err := s.store.GrantAccess(ctx, telegramID, srv.ID); err != nil {
			return store.User{}, err
		}
	}
	s.log.Info("user added", "user", telegramID)
	return u, nil
}

func (s *Service) AdminDisableUser(ctx context.Context, telegramID int64) error {
	if err := s.store.SetUserEnabled(ctx, telegramID, false); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.revokeUserPeers(ctx, telegramID)
	s.log.Info("user disabled", "user", telegramID)
	return nil
}

// revokeUserPeers отзывает все активные peer'ы пользователя на серверах
// и в БД. Лучше-постараемся: пользователь уже отключён, ошибки очистки
// только логируются (peer можно добить повторным disable или ручным delete).
func (s *Service) revokeUserPeers(ctx context.Context, telegramID int64) {
	peers, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		s.log.Error("list peers on disable failed", "user", telegramID, "err", err)
		return
	}
	for _, p := range peers {
		if err := s.vpn.RemovePeer(ctx, p.ServerID, p.PeerID); err != nil {
			s.log.Error("peer remove on disable failed", "user", telegramID, "peer_db_id", p.ID, "err", err)
			continue
		}
		if err := s.store.RevokePeer(ctx, p.ID); err != nil {
			s.log.Error("revoke on disable failed", "user", telegramID, "peer_db_id", p.ID, "err", err)
		}
	}
}

func (s *Service) AdminSetLimit(ctx context.Context, telegramID int64, limit int) error {
	if limit < 1 || limit > 1000 {
		return ErrBadLimit
	}
	if err := s.store.SetUserLimit(ctx, telegramID, limit); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.log.Info("limit changed", "user", telegramID, "limit", limit)
	return nil
}

type UserInfo struct {
	store.User
	ActiveConfigs int
}

func (s *Service) AdminListUsers(ctx context.Context) ([]UserInfo, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	peers, err := s.store.ListActivePeersAll(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	for _, p := range peers {
		counts[p.TelegramID]++
	}
	out := make([]UserInfo, 0, len(users))
	for _, u := range users {
		out = append(out, UserInfo{User: u, ActiveConfigs: counts[u.TelegramID]})
	}
	return out, nil
}
