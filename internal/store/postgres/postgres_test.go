package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"amnezia-manager-bot/internal/db"
	"amnezia-manager-bot/internal/store"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("set TEST_POSTGRES=1 to run integration tests")
	}
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:54329/amnezia_test?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, user_server_access, vpn_peers, server_status_messages RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return New(pool)
}

func TestUserLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.UpsertUser(ctx, store.User{TelegramID: 1, Username: "u", Enabled: true, ConfigLimit: 3}); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 3 {
		t.Fatalf("get: %v %+v", err, u)
	}
	if err := s.SetUserLimit(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUsername(ctx, 1, "newname"); err != nil {
		t.Fatal(err)
	}
	u, err = s.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 7 || u.Enabled || u.Username != "newname" {
		t.Fatalf("updated: %v %+v", err, u)
	}
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("list: %v %d", err, len(users))
	}
	if err := s.SetUserEnabled(ctx, 99, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAccess(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = s.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 1})
	if ok, _ := s.HasAccess(ctx, 1, "s1"); ok {
		t.Fatal("unexpected access")
	}
	if err := s.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatalf("idempotent grant: %v", err)
	}
	if ok, _ := s.HasAccess(ctx, 1, "s1"); !ok {
		t.Fatal("no access after grant")
	}
	srvs, err := s.ListUserServers(ctx, 1)
	if err != nil || len(srvs) != 1 || srvs[0] != "s1" {
		t.Fatalf("ListUserServers: %v %v", err, srvs)
	}
}

func TestPeers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = s.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 3})
	p, err := s.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB1", DeviceName: "phone", ClientIP: "10.8.1.2"})
	if err != nil || p.ID == 0 || p.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, p)
	}
	if _, err := s.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB2", DeviceName: "pc", ClientIP: "10.8.1.2"}); err == nil {
		t.Fatal("duplicate client_ip must fail (unique constraint)")
	}
	got, err := s.GetActivePeer(ctx, 1, p.ID)
	if err != nil || got.ClientIP != "10.8.1.2" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := s.GetActivePeer(ctx, 2, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign peer must be ErrNotFound, got %v", err)
	}
	if n := len(listActive(t, s, 1)); n != 1 {
		t.Fatalf("active = %d", n)
	}
	if err := s.RevokePeer(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(listActive(t, s, 1)); n != 0 {
		t.Fatalf("active after revoke = %d", n)
	}
	onServer, err := s.ListActivePeersOnServer(ctx, "s1")
	if err != nil || len(onServer) != 0 {
		t.Fatalf("on server: %v %d", err, len(onServer))
	}
}

func listActive(t *testing.T, s *Store, uid int64) []store.Peer {
	t.Helper()
	ps, err := s.ListActivePeers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestStatusMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.GetStatusMessage(ctx, "s1", 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 200}); err != nil {
		t.Fatal(err)
	}
	sm, err := s.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID != 200 {
		t.Fatalf("get: %v %+v", err, sm)
	}
}
