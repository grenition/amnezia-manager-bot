package vpn

import (
	"strings"
	"testing"

	"amnezia-manager-bot/internal/config"
)

func TestBuildClientConfig(t *testing.T) {
	srv := config.ServerConfig{
		ID: "s1", Endpoint: "vpn.example.com:51820", ServerPublicKey: "SRVPUB",
		DNS: []string{"1.1.1.1"},
		AWG: &config.AWGParams{Jc: 4, Jmin: 40, Jmax: 70, S1: 68, S2: 149, H1: 1, H2: 2, H3: 3, H4: 4},
	}
	cfg := BuildClientConfig(srv, "PRIV", "10.8.1.7")
	for _, want := range []string{
		"Address = 10.8.1.7/32",
		"PrivateKey = PRIV",
		"DNS = 1.1.1.1",
		"Jc = 4", "Jmin = 40", "Jmax = 70", "S1 = 68", "S2 = 149", "H1 = 1", "H2 = 2", "H3 = 3", "H4 = 4",
		"PublicKey = SRVPUB",
		"AllowedIPs = 0.0.0.0/0",
		"Endpoint = vpn.example.com:51820",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "::") {
		t.Fatal("ipv6 must not appear in client config")
	}
}

func TestBuildClientConfigMinimal(t *testing.T) {
	cfg := BuildClientConfig(config.ServerConfig{Endpoint: "e:1", ServerPublicKey: "K"}, "P", "10.0.0.2")
	if strings.Contains(cfg, "DNS") || strings.Contains(cfg, "Jc =") {
		t.Fatalf("optional sections must be omitted:\n%s", cfg)
	}
	if !strings.Contains(cfg, "AllowedIPs = 0.0.0.0/0") {
		t.Fatal("placeholder AllowedIPs required")
	}
}
