package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/store"
	"amnezia-manager-bot/internal/store/memory"
	"amnezia-manager-bot/internal/vpn"
)

type fakeVPN struct {
	mu      sync.Mutex
	created map[string]string // pub -> ip
	removed []string
	errNew  error
	errDel  error
}

func newFakeVPN() *fakeVPN { return &fakeVPN{created: map[string]string{}} }

func (f *fakeVPN) CreatePeer(_ context.Context, _, pub, ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errNew != nil {
		return f.errNew
	}
	f.created[pub] = ip
	return nil
}

func (f *fakeVPN) RemovePeer(_ context.Context, _, pub string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errDel != nil {
		return f.errDel
	}
	f.removed = append(f.removed, pub)
	return nil
}

func (f *fakeVPN) HealthCheck(context.Context, string) error { return nil }

type fakeIPs struct{ list []string }

func (f fakeIPs) AllowedIPs() []string { return f.list }

func testCfg() config.Config {
	return config.Config{
		DefaultLimit: 2,
		Servers: []config.ServerConfig{{
			ID: "s1", Enabled: true, DisplayName: "S1", Host: "10.0.0.1", SSHUser: "bot",
			Endpoint: "1.2.3.4:51820", ServerPublicKey: "SRV", ClientCIDR: "10.8.1.0/24",
		}},
	}
}

func newSvc(t *testing.T) (*Service, *memory.MemoryStore, *fakeVPN) {
	t.Helper()
	st := memory.New()
	fv := newFakeVPN()
	svc := New(testCfg(), st, fv, fakeIPs{list: []string{"1.0.0.0/8", "2.0.0.0/7"}}, slog.Default())
	ctx := context.Background()
	_ = st.UpsertUser(ctx, store.User{TelegramID: 100, Username: "u100", Enabled: true, ConfigLimit: 2})
	_ = st.GrantAccess(ctx, 100, "s1")
	return svc, st, fv
}

func TestCreateConfigHappyPath(t *testing.T) {
	svc, st, fv := newSvc(t)
	ctx := context.Background()
	cc, err := svc.CreateConfig(ctx, 100, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if cc.FileName != "phone.conf" {
		t.Fatalf("file name %q", cc.FileName)
	}
	if !strings.Contains(cc.Content, "AllowedIPs = 1.0.0.0/8, 2.0.0.0/7") {
		t.Fatalf("allowed ips not patched:\n%s", cc.Content)
	}
	if strings.Contains(cc.Content, "0.0.0.0/0") {
		t.Fatal("placeholder leaked into final config")
	}
	if !strings.Contains(cc.Content, "[Interface]") || !strings.Contains(cc.Content, "PrivateKey = ") {
		t.Fatal("config incomplete")
	}
	peers, err := st.ListActivePeers(ctx, 100)
	if err != nil || len(peers) != 1 {
		t.Fatalf("peers: %v %d", err, len(peers))
	}
	if peers[0].ClientIP != "10.8.1.2" || peers[0].DeviceName != "phone" {
		t.Fatalf("peer %+v", peers[0])
	}
	if len(fv.created) != 1 {
		t.Fatalf("vpn created %v", fv.created)
	}
	for pub, ip := range fv.created {
		if peers[0].PeerID != pub || ip != "10.8.1.2" {
			t.Fatalf("mismatch pub=%q ip=%q", pub, ip)
		}
	}
}

func TestCreateConfigSecondIP(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 100, "aaaa"); err != nil {
		t.Fatal(err)
	}
	cc, err := svc.CreateConfig(ctx, 100, "bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cc.Content, "Address = 10.8.1.3/32") {
		t.Fatalf("second ip not allocated:\n%s", cc.Content)
	}
}

func TestCreateConfigErrors(t *testing.T) {
	svc, st, fv := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 999, "xxxx"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := svc.CreateConfig(ctx, 100, "ab"); !errors.Is(err, ErrBadDeviceName) {
		t.Fatalf("short name: %v", err)
	}
	if _, err := svc.CreateConfig(ctx, 100, "привет"); !errors.Is(err, ErrBadDeviceName) {
		t.Fatalf("non-ascii name: %v", err)
	}
	if _, err := svc.CreateConfig(ctx, 100, "dev1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateConfig(ctx, 100, "dev2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateConfig(ctx, 100, "dev3"); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("limit: %v", err)
	}
	_ = st.UpsertUser(ctx, store.User{TelegramID: 200, Enabled: false, ConfigLimit: 2})
	if _, err := svc.CreateConfig(ctx, 200, "xxxx"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("disabled: %v", err)
	}
	_ = st.UpsertUser(ctx, store.User{TelegramID: 300, Enabled: true, ConfigLimit: 2})
	if _, err := svc.CreateConfig(ctx, 300, "xxxx"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("no access: %v", err)
	}
	// сбой VPN — ничего не сохраняем
	_ = st.SetUserLimit(ctx, 100, 5)
	fv.errNew = errors.New("ssh down")
	if _, err := svc.CreateConfig(ctx, 100, "zzzz"); err == nil {
		t.Fatal("expected vpn error")
	}
	fv.errNew = nil
	peers, _ := st.ListActivePeers(ctx, 100)
	if len(peers) != 2 {
		t.Fatalf("store must have only 2 peers, got %d", len(peers))
	}
}

func TestCheckAccess(t *testing.T) {
	svc, st, _ := newSvc(t)
	ctx := context.Background()
	if err := svc.CheckAccess(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAccess(ctx, 999); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("want ErrNoAccess, got %v", err)
	}
	_ = st.SetUserEnabled(ctx, 100, false)
	if err := svc.CheckAccess(ctx, 100); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("disabled: %v", err)
	}
	if svc.IsAdmin(100) {
		t.Fatal("100 is not admin")
	}
}

func TestListDevices(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 100, "aaaa"); err != nil {
		t.Fatal(err)
	}
	peers, limit, err := svc.ListDevices(ctx, 100)
	if err != nil || limit != 2 || len(peers) != 1 || peers[0].DeviceName != "aaaa" {
		t.Fatalf("%v %d %v", peers, limit, err)
	}
	if _, _, err := svc.ListDevices(ctx, 999); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("want ErrNoAccess, got %v", err)
	}
}

func TestDeleteConfig(t *testing.T) {
	svc, _, fv := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 100, "dev"); err != nil {
		t.Fatal(err)
	}
	peers, _, _ := svc.ListDevices(ctx, 100)
	p := peers[0]

	if err := svc.DeleteConfig(ctx, 100, p.ID); err != nil {
		t.Fatal(err)
	}
	if len(fv.removed) != 1 || fv.removed[0] != p.PeerID {
		t.Fatalf("removed %v", fv.removed)
	}
	if peers, _, _ = svc.ListDevices(ctx, 100); len(peers) != 0 {
		t.Fatalf("peer not revoked")
	}

	// чужой peer
	if _, err := svc.CreateConfig(ctx, 100, "dev2"); err != nil {
		t.Fatal(err)
	}
	peers, _, _ = svc.ListDevices(ctx, 100)
	if err := svc.DeleteConfig(ctx, 999, peers[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign: %v", err)
	}

	// сбой VPN — не помечаем отозванным в БД
	fv.errDel = errors.New("ssh down")
	if err := svc.DeleteConfig(ctx, 100, peers[0].ID); err == nil {
		t.Fatal("expected vpn error")
	}
	fv.errDel = nil
	if peers, _, _ = svc.ListDevices(ctx, 100); len(peers) != 1 {
		t.Fatal("peer must stay active when vpn remove fails")
	}
}

func TestServerForComplaint(t *testing.T) {
	svc, _, _ := newSvc(t)
	id, name, err := svc.ServerForComplaint(context.Background(), 100)
	if err != nil || id != "s1" || name != "S1" {
		t.Fatalf("%q %q %v", id, name, err)
	}
	if _, _, err := svc.ServerForComplaint(context.Background(), 999); err != nil {
		t.Fatalf("complaint server должен работать и для unknown user: %v", err)
	}
}

var _ vpn.Provider = (*fakeVPN)(nil)
