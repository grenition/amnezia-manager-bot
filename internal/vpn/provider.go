package vpn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"amnezia-manager-bot/internal/config"
)

var ErrServerNotFound = errors.New("vpn server not found")

// Provider управляет peer'ами на AmneziaWG-серверах (реализация — sshprovider).
type Provider interface {
	CreatePeer(ctx context.Context, serverID, publicKey, clientIP string) error
	RemovePeer(ctx context.Context, serverID, publicKey string) error
	HealthCheck(ctx context.Context, serverID string) error
}

// BuildClientConfig собирает клиентский конфиг AmneziaWG.
// AllowedIPs здесь плейсхолдер — сервис заменяет его через patcher.Patch.
func BuildClientConfig(srv config.ServerConfig, clientPrivateKey, clientIP string) string {
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "Address = %s/32\n", clientIP)
	fmt.Fprintf(&b, "PrivateKey = %s\n", clientPrivateKey)
	if len(srv.DNS) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(srv.DNS, ", "))
	}
	if p := srv.AWG; p != nil {
		fmt.Fprintf(&b, "Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n",
			p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.H1, p.H2, p.H3, p.H4)
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", srv.ServerPublicKey)
	b.WriteString("AllowedIPs = 0.0.0.0/0\n")
	fmt.Fprintf(&b, "Endpoint = %s\n", srv.Endpoint)
	b.WriteString("PersistentKeepalive = 25\n")
	return b.String()
}
