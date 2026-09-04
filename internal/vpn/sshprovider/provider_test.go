package sshprovider

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/vpn"
)

func testCfg() config.Config {
	return config.Config{
		Servers: []config.ServerConfig{
			{ID: "s1", Enabled: true, Host: "10.0.0.1", SSHPort: 22, SSHUser: "bot", Interface: "wg0"},
		},
	}
}

type fakeClient struct {
	cmds []string
	run  func(cmd string) (string, error)
}

func (f *fakeClient) Run(_ context.Context, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	if f.run != nil {
		return f.run(cmd)
	}
	return "ok", nil
}

func (f *fakeClient) Close() error { return nil }

func newProvider(t *testing.T) (*Provider, *fakeClient) {
	t.Helper()
	fc := &fakeClient{}
	p := New(testCfg(), slog.Default())
	p.dial = func(config.ServerConfig) (sshClient, error) { return fc, nil }
	return p, fc
}

func TestCreatePeerCommand(t *testing.T) {
	p, fc := newProvider(t)
	if err := p.CreatePeer(context.Background(), "s1", "PUBKEY==", "10.8.1.5"); err != nil {
		t.Fatal(err)
	}
	want := "sudo awg-peer-add wg0 PUBKEY== 10.8.1.5/32"
	if len(fc.cmds) != 1 || fc.cmds[0] != want {
		t.Fatalf("cmds = %v, want %q", fc.cmds, want)
	}
}

func TestRemovePeerCommand(t *testing.T) {
	p, fc := newProvider(t)
	if err := p.RemovePeer(context.Background(), "s1", "PUBKEY=="); err != nil {
		t.Fatal(err)
	}
	want := "sudo awg-peer-remove wg0 PUBKEY=="
	if len(fc.cmds) != 1 || fc.cmds[0] != want {
		t.Fatalf("cmds = %v, want %q", fc.cmds, want)
	}
}

func TestRemovePeerNotFoundIsSuccess(t *testing.T) {
	p, fc := newProvider(t)
	fc.run = func(string) (string, error) { return "PEER_NOT_FOUND", errors.New("exit status 3") }
	if err := p.RemovePeer(context.Background(), "s1", "PUBKEY=="); err != nil {
		t.Fatalf("PEER_NOT_FOUND must be treated as already-removed success: %v", err)
	}
}

func TestRemovePeerOtherErrorFails(t *testing.T) {
	p, fc := newProvider(t)
	fc.run = func(string) (string, error) { return "sudo: some other failure", errors.New("exit status 1") }
	if err := p.RemovePeer(context.Background(), "s1", "PUBKEY=="); err == nil {
		t.Fatal("generic errors must stay errors")
	}
}

func TestHealthCheck(t *testing.T) {
	p, _ := newProvider(t)
	if err := p.HealthCheck(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	p2, fc2 := newProvider(t)
	fc2.run = func(string) (string, error) { return "", errors.New("dial fail") }
	if err := p2.HealthCheck(context.Background(), "s1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnknownServer(t *testing.T) {
	p, _ := newProvider(t)
	err := p.CreatePeer(context.Background(), "nope", "P", "10.0.0.2")
	if !errors.Is(err, vpn.ErrServerNotFound) {
		t.Fatalf("want ErrServerNotFound, got %v", err)
	}
}

func TestErrorContainsOutput(t *testing.T) {
	p, fc := newProvider(t)
	fc.run = func(string) (string, error) { return "some stderr", errors.New("exit status 1") }
	err := p.CreatePeer(context.Background(), "s1", "PUBKEY==", "10.8.1.5")
	if err == nil || !strings.Contains(err.Error(), "some stderr") {
		t.Fatalf("err = %v", err)
	}
}
