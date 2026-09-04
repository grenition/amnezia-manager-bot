package sshprovider

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/vpn"
)

type sshClient interface {
	Run(ctx context.Context, cmd string) (string, error)
	Close() error
}

type dialFunc func(srv config.ServerConfig) (sshClient, error)

// Provider управляет peer'ами AmneziaWG через SSH и sudo-скрипты.
type Provider struct {
	cfg  config.Config
	log  *slog.Logger
	dial dialFunc

	mu    sync.Mutex
	conns map[string]sshClient
}

func New(cfg config.Config, log *slog.Logger) *Provider {
	return &Provider{cfg: cfg, log: log, dial: realDial, conns: map[string]sshClient{}}
}

func (p *Provider) client(serverID string) (sshClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[serverID]; ok {
		return c, nil
	}
	srv, ok := p.cfg.ServerByID(serverID)
	if !ok || !srv.Enabled {
		return nil, vpn.ErrServerNotFound
	}
	c, err := p.dial(srv)
	if err != nil {
		return nil, err
	}
	p.conns[serverID] = c
	return c, nil
}

func (p *Provider) run(ctx context.Context, serverID, cmd string) (string, error) {
	c, err := p.client(serverID)
	if err != nil {
		return "", err
	}
	out, err := c.Run(ctx, cmd)
	if err != nil {
		p.mu.Lock()
		delete(p.conns, serverID)
		p.mu.Unlock()
		if cerr := c.Close(); cerr != nil {
			p.log.Warn("close evicted ssh connection", "server", serverID, "error", cerr)
		}
		return out, fmt.Errorf("ssh run %q: %w (output: %s)", cmd, err, out)
	}
	return out, nil
}

func (p *Provider) CreatePeer(ctx context.Context, serverID, publicKey, clientIP string) error {
	srv, ok := p.cfg.ServerByID(serverID)
	if !ok {
		return vpn.ErrServerNotFound
	}
	_, err := p.run(ctx, serverID, fmt.Sprintf("sudo awg-peer-add %s %s %s/32", srv.Interface, publicKey, clientIP))
	return err
}

func (p *Provider) RemovePeer(ctx context.Context, serverID, publicKey string) error {
	srv, ok := p.cfg.ServerByID(serverID)
	if !ok {
		return vpn.ErrServerNotFound
	}
	_, err := p.run(ctx, serverID, fmt.Sprintf("sudo awg-peer-remove %s %s", srv.Interface, publicKey))
	return err
}

func (p *Provider) HealthCheck(ctx context.Context, serverID string) error {
	srv, ok := p.cfg.ServerByID(serverID)
	if !ok {
		return vpn.ErrServerNotFound
	}
	out, err := p.run(ctx, serverID, fmt.Sprintf("sudo awg-health %s", srv.Interface))
	if err != nil {
		return err
	}
	if out != "ok\n" && out != "ok" {
		return fmt.Errorf("unexpected health output %q", out)
	}
	return nil
}

func realDial(srv config.ServerConfig) (sshClient, error) {
	keyData, err := os.ReadFile(os.Getenv("SSH_PRIVATE_KEY"))
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	addr := net.JoinHostPort(srv.Host, fmt.Sprintf("%d", srv.SSHPort))
	c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            srv.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: pin host key в проде
		Timeout:         10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return &realClient{c: c}, nil
}

type realClient struct {
	c *ssh.Client
}

func (r *realClient) Run(ctx context.Context, cmd string) (string, error) {
	sess, err := r.c.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = sess.Close() }()
	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out
	done := make(chan error, 1)
	if err := sess.Start(cmd); err != nil {
		return out.String(), err
	}
	go func() { done <- sess.Wait() }()
	select {
	case err := <-done:
		return out.String(), err
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return out.String(), ctx.Err()
	}
}

func (r *realClient) Close() error { return r.c.Close() }
