package config

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validYAML = `
admin_ids: [10, 20]
default_limit: 3
routes:
  url: https://example.com/wg-allowed-ips.txt
  refresh_interval: 1h
monitor:
  check_interval: 30s
  down_threshold: 2m
servers:
  - id: s1
    display_name: S1
    enabled: true
    host: 10.0.0.1
    ssh_user: bot
    endpoint: 1.2.3.4:51820
    server_public_key: AAAA
    client_cidr: 10.8.1.0/24
    awg:
      jc: 4
      jmin: 40
      jmax: 70
      s1: 68
      s2: 149
      h1: 11
      h2: 22
      h3: 33
      h4: 44
`

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("BOT_TOKEN", "123:abc")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("SSH_PRIVATE_KEY", "/tmp/id_ed25519")
}

func TestLoadOK(t *testing.T) {
	setEnvs(t)
	cfg, err := Load(writeCfg(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLimit != 3 || len(cfg.AdminIDs) != 2 {
		t.Fatalf("bad cfg: %+v", cfg)
	}
	if cfg.Servers[0].SSHPort != 22 || cfg.Servers[0].Interface != "wg0" {
		t.Fatalf("defaults not applied: %+v", cfg.Servers[0])
	}
	if p := cfg.Servers[0].AWG; p == nil || p.Jc != 4 || p.Jmin != 40 || p.H4 != 44 {
		t.Fatalf("awg params not parsed: %+v", cfg.Servers[0].AWG)
	}
	if cfg.Monitor.CheckInterval != 30*time.Second {
		t.Fatalf("monitor parse failed: %+v", cfg.Monitor)
	}
	srv, err := cfg.DefaultServer()
	if err != nil || srv.ID != "s1" {
		t.Fatalf("DefaultServer: %v %+v", err, srv)
	}
	if _, ok := cfg.ServerByID("s1"); !ok {
		t.Fatal("ServerByID failed")
	}
	if n := len(cfg.EnabledServers()); n != 1 {
		t.Fatalf("EnabledServers = %d", n)
	}
}

func TestLoadErrors(t *testing.T) {
	setEnvs(t)
	cases := map[string]string{
		"no servers":  "admin_ids: [1]\ndefault_limit: 1\n",
		"dup id":      validYAML + "\n  - id: s1\n    display_name: X\n    endpoint: 1.2.3.4:5\n    server_public_key: B\n    client_cidr: 10.9.0.0/24\n",
		"bad cidr":    validYAML + "\n  - id: s2\n    display_name: X\n    enabled: true\n    host: 10.0.0.2\n    ssh_user: bot\n    endpoint: 1.2.3.4:5\n    server_public_key: B\n    client_cidr: nope\n",
		"no endpoint": "admin_ids: [1]\ndefault_limit: 1\nservers:\n  - id: s1\n    display_name: S\n    server_public_key: A\n    client_cidr: 10.0.0.0/24\n",
		"no admins":   "default_limit: 1\nservers:\n  - id: s1\n    display_name: S\n    endpoint: 1.2.3.4:5\n    server_public_key: A\n    client_cidr: 10.0.0.0/24\n",
		"bad limit":   "admin_ids: [1]\ndefault_limit: 0\nservers:\n  - id: s1\n    display_name: S\n    endpoint: 1.2.3.4:5\n    server_public_key: A\n    client_cidr: 10.0.0.0/24\n",
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, y)); err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}

func TestLoadEnvMissing(t *testing.T) {
	t.Setenv("BOT_TOKEN", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SSH_PRIVATE_KEY", "")
	if _, err := Load(writeCfg(t, validYAML)); err == nil {
		t.Fatal("expected env error")
	}
}

func TestClientCIDRParse(t *testing.T) {
	setEnvs(t)
	cfg, err := Load(writeCfg(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := net.ParseCIDR(cfg.Servers[0].ClientCIDR); err != nil {
		t.Fatalf("client_cidr invalid: %v", err)
	}
}
