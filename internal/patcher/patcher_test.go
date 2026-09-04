package patcher

import (
	"errors"
	"strings"
	"testing"
)

const sampleCfg = "[Interface]\nAddress = 10.8.1.2/32\nPrivateKey = PRIV\n\n[Peer]\nPublicKey = SRVPUB\nAllowedIPs = 0.0.0.0/0\nEndpoint = 1.2.3.4:51820\n"

func TestPatch(t *testing.T) {
	out, err := Patch(sampleCfg, []string{"1.0.0.0/8", "2.0.0.0/7"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "AllowedIPs = 1.0.0.0/8, 2.0.0.0/7") {
		t.Fatalf("patched:\n%s", out)
	}
	if !strings.Contains(out, "PrivateKey = PRIV") || !strings.Contains(out, "Endpoint = 1.2.3.4:51820") {
		t.Fatalf("other lines must be preserved:\n%s", out)
	}
	if strings.Contains(out, "0.0.0.0/0") {
		t.Fatalf("old value must be replaced:\n%s", out)
	}
}

func TestPatchNoLine(t *testing.T) {
	if _, err := Patch("no allowed ips here", nil); !errors.Is(err, ErrNoAllowedIPs) {
		t.Fatalf("want ErrNoAllowedIPs, got %v", err)
	}
}

func TestValidateAndClean(t *testing.T) {
	got, err := ValidateAndClean([]string{"2.0.0.0/7", "1.0.0.0/8", " 1.0.0.0/8 "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "1.0.0.0/8" {
		t.Fatalf("got %v", got)
	}
}

func TestValidateAndCleanRejects(t *testing.T) {
	cases := map[string][]string{
		"zero route":  {"0.0.0.0/0"},
		"private":     {"1.0.0.0/8", "10.0.0.0/8"},
		"172 private": {"172.16.0.0/12"},
		"192 private": {"192.168.0.0/16"},
		"loopback":    {"1.0.0.0/8", "127.0.0.0/8"},
		"link-local":  {"169.254.0.0/16"},
		"multicast":   {"224.0.0.0/4"},
		"cgnat":       {"100.64.0.0/10"},
		"ipv6":        {"2001:db8::/32"},
		"garbage":     {"not-a-cidr", "1.2.3.4"},
		"empty":       {"", "  "},
		"host bits":   {"1.0.0.1/8"},
	}
	for name, list := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateAndClean(list); err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}
