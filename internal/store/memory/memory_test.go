package memory

import (
	"context"
	"errors"
	"testing"

	"amnezia-manager-bot/internal/store"
)

func TestUserCRUD(t *testing.T) {
	m := New()
	ctx := context.Background()
	if _, err := m.GetUser(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := m.UpsertUser(ctx, store.User{TelegramID: 1, Username: "u", Enabled: true, ConfigLimit: 3}); err != nil {
		t.Fatal(err)
	}
	u, err := m.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 3 {
		t.Fatalf("get: %v %+v", err, u)
	}
	if err := m.SetUserLimit(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.ConfigLimit != 5 {
		t.Fatal("limit not updated")
	}
	if err := m.SetUserEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.Enabled {
		t.Fatal("not disabled")
	}
	if err := m.SetUsername(ctx, 1, "new"); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.Username != "new" {
		t.Fatal("username not updated")
	}
	if err := m.SetUserEnabled(ctx, 42, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAccess(t *testing.T) {
	m := New()
	ctx := context.Background()
	_ = m.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 1})
	if ok, _ := m.HasAccess(ctx, 1, "s1"); ok {
		t.Fatal("unexpected access")
	}
	if err := m.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.HasAccess(ctx, 1, "s1"); !ok {
		t.Fatal("no access after grant")
	}
	srvs, err := m.ListUserServers(ctx, 1)
	if err != nil || len(srvs) != 1 || srvs[0] != "s1" {
		t.Fatalf("ListUserServers: %v %v", err, srvs)
	}
}

func TestPeers(t *testing.T) {
	m := New()
	ctx := context.Background()
	_ = m.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 2})
	p, err := m.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB", DeviceName: "phone", ClientIP: "10.8.1.2"})
	if err != nil || p.ID == 0 || p.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, p)
	}
	got, err := m.GetActivePeer(ctx, 1, p.ID)
	if err != nil || got.DeviceName != "phone" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := m.GetActivePeer(ctx, 2, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign peer must be not found, got %v", err)
	}
	if n := len(mustPeers(t, m, 1)); n != 1 {
		t.Fatalf("active = %d", n)
	}
	if err := m.RevokePeer(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(mustPeers(t, m, 1)); n != 0 {
		t.Fatalf("active after revoke = %d", n)
	}
	if _, err := m.GetActivePeer(ctx, 1, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked peer must be not found, got %v", err)
	}
	onServer, err := m.ListActivePeersOnServer(ctx, "s1")
	if err != nil || len(onServer) != 0 {
		t.Fatalf("ListActivePeersOnServer: %v %d", err, len(onServer))
	}
	all, err := m.ListActivePeersAll(ctx)
	if err != nil || len(all) != 0 {
		t.Fatalf("ListActivePeersAll: %v %d", err, len(all))
	}
}

func mustPeers(t *testing.T, m *MemoryStore, uid int64) []store.Peer {
	t.Helper()
	ps, err := m.ListActivePeers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestStatusMessages(t *testing.T) {
	m := New()
	ctx := context.Background()
	if _, err := m.GetStatusMessage(ctx, "s1", 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := m.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 100}); err != nil {
		t.Fatal(err)
	}
	sm, err := m.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID != 100 {
		t.Fatalf("get: %v %+v", err, sm)
	}
	if err := m.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 200}); err != nil {
		t.Fatal(err)
	}
	if sm, _ = m.GetStatusMessage(ctx, "s1", 10); sm.MessageID != 200 {
		t.Fatal("upsert failed")
	}
}

func TestKnownUsers(t *testing.T) {
	m := New()
	ctx := context.Background()
	if _, err := m.FindKnownUser(ctx, "ivan"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := m.UpsertKnownUser(ctx, 1, "ivan", "Ivan"); err != nil {
		t.Fatal(err)
	}
	got, err := m.FindKnownUser(ctx, "ivan")
	if err != nil || got.TelegramID != 1 || got.FirstName != "Ivan" {
		t.Fatalf("find: %v %+v", err, got)
	}
	if err := m.UpsertKnownUser(ctx, 2, "ivan", "Ivan2"); err != nil {
		t.Fatal(err)
	}
	got, err = m.FindKnownUser(ctx, "ivan")
	if err != nil || got.TelegramID != 2 {
		t.Fatalf("username must move to user 2: %v %+v", err, got)
	}
	old, err := m.FindKnownUser(ctx, "1")
	if err != nil || old.TelegramID != 1 {
		t.Fatalf("old user keeps id-username: %v %+v", err, old)
	}
	if err := m.UpsertKnownUser(ctx, 1, "ivan_new", "Ivan"); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.FindKnownUser(ctx, "ivan_new"); got.TelegramID != 1 {
		t.Fatal("username update failed")
	}
}
