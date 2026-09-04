package service

import (
	"context"
	"errors"
	"testing"
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
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.AdminAddUser(ctx, 555, "vasya"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdminDisableUser(ctx, 555); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAccess(ctx, 555); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("want ErrNoAccess, got %v", err)
	}
	if _, err := svc.CreateConfig(ctx, 555, "dev"); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("create must fail: %v", err)
	}
	if err := svc.AdminDisableUser(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
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
	if err := svc.AdminSetLimit(ctx, 100, 51); !errors.Is(err, ErrBadLimit) {
		t.Fatalf("huge limit: %v", err)
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
