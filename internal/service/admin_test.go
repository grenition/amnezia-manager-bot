package service

import (
	"context"
	"errors"
	"testing"

	"amnezia-manager-bot/internal/store"
)

func TestAdminAddUser(t *testing.T) {
	svc, st, _ := newSvc(t)
	ctx := context.Background()
	u, err := svc.AdminAddUser(ctx, 555, "vasya")
	if err != nil {
		t.Fatal(err)
	}
	if u.ConfigLimit != 2 || !u.Enabled {
		t.Fatalf("%+v", u)
	}
	got, _ := st.GetUser(ctx, 555)
	if got.Username != "vasya" || got.ConfigLimit != 2 {
		t.Fatalf("%+v", got)
	}
	ok, _ := st.HasAccess(ctx, 555, "s1")
	if !ok {
		t.Fatal("access to all enabled servers must be granted")
	}
	if err := svc.CheckAccess(ctx, 555); err != nil {
		t.Fatal(err)
	}
}

func TestAdminDisableUser(t *testing.T) {
	svc, st, fv := newSvc(t)
	ctx := context.Background()
	if _, err := svc.AdminAddUser(ctx, 555, "vasya"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateConfig(ctx, 555, "dev"); err != nil {
		t.Fatal(err)
	}
	peers, _, err := svc.ListDevices(ctx, 555)
	if err != nil || len(peers) != 1 {
		t.Fatalf("devices: %v %d %v", peers, len(peers), err)
	}
	p := peers[0]

	if err := svc.AdminDisableUser(ctx, 555); err != nil {
		t.Fatal(err)
	}
	if ps, _ := st.ListActivePeers(ctx, 555); len(ps) != 0 {
		t.Fatalf("peers must be revoked on disable, got %d", len(ps))
	}
	if len(fv.removed) != 1 || fv.removed[0] != p.PeerID {
		t.Fatalf("vpn removed %v, want [%s]", fv.removed, p.PeerID)
	}
	if err := svc.CheckAccess(ctx, 555); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("want ErrNoAccess, got %v", err)
	}
	if _, err := svc.CreateConfig(ctx, 555, "dev2"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("create must fail: %v", err)
	}
	if err := svc.AdminDisableUser(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestAdminDisableUserPeerRemoveFails(t *testing.T) {
	svc, st, fv := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 100, "dev"); err != nil {
		t.Fatal(err)
	}
	fv.errDel = errors.New("ssh down")
	if err := svc.AdminDisableUser(ctx, 100); err != nil {
		t.Fatalf("disable must succeed despite peer cleanup failure: %v", err)
	}
	if ps, _ := st.ListActivePeers(ctx, 100); len(ps) != 1 {
		t.Fatal("peer must stay active when vpn remove fails")
	}
	if err := svc.CheckAccess(ctx, 100); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("want ErrNoAccess, got %v", err)
	}
}

func TestAdminSetLimit(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if err := svc.AdminSetLimit(ctx, 100, 5); err != nil {
		t.Fatal(err)
	}
	_, limit, _ := svc.ListDevices(ctx, 100)
	if limit != 5 {
		t.Fatalf("limit = %d", limit)
	}
	if err := svc.AdminSetLimit(ctx, 100, 0); !errors.Is(err, ErrBadLimit) {
		t.Fatalf("zero limit: %v", err)
	}
	if err := svc.AdminSetLimit(ctx, 100, 1001); !errors.Is(err, ErrBadLimit) {
		t.Fatalf("huge limit: %v", err)
	}
	if err := svc.AdminSetLimit(ctx, 100, 1000); err != nil {
		t.Fatalf("max limit must be allowed: %v", err)
	}
	if err := svc.AdminSetLimit(ctx, 999, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestAdminListUsers(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateConfig(ctx, 100, "aaaa"); err != nil {
		t.Fatal(err)
	}
	users, err := svc.AdminListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d", len(users))
	}
	if users[0].TelegramID != 100 || users[0].ActiveConfigs != 1 {
		t.Fatalf("%+v", users[0])
	}
}

func TestInviteRedemption(t *testing.T) {
	svc, st, _ := newSvc(t)
	ctx := context.Background()
	if err := svc.CreateInvite(ctx, "@NewGuy"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FindKnownUser(ctx, "newguy"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("known user must not exist yet: %v", err)
	}
	svc.RememberUser(ctx, 777, "NewGuy", "Новый")
	if err := svc.CheckAccess(ctx, 777); err != nil {
		t.Fatalf("invite must grant access on first contact: %v", err)
	}
	u, err := st.GetUser(ctx, 777)
	if err != nil || u.Username != "newguy" || !u.Enabled {
		t.Fatalf("redeemed user: %v %+v", err, u)
	}
	if invites, _ := svc.ListInvites(ctx); len(invites) != 0 {
		t.Fatal("invite must be consumed")
	}
	svc.RememberUser(ctx, 778, "newguy", "Другой")
	if err := svc.CheckAccess(ctx, 778); err == nil {
		t.Fatal("invite must not redeem twice")
	}
}

func TestCreateInviteValidation(t *testing.T) {
	svc, _, _ := newSvc(t)
	if err := svc.CreateInvite(context.Background(), "  "); err == nil {
		t.Fatal("empty username must fail")
	}
	if err := svc.CreateInvite(context.Background(), "a b"); err == nil {
		t.Fatal("username with space must fail")
	}
	if err := svc.CreateInvite(context.Background(), "@ok_user"); err != nil {
		t.Fatalf("valid username must pass: %v", err)
	}
}
