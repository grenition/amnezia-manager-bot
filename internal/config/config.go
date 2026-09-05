package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type AWGParams struct {
	Jc   int `yaml:"jc"`
	Jmin int `yaml:"jmin"`
	Jmax int `yaml:"jmax"`
	S1   int `yaml:"s1"`
	S2   int `yaml:"s2"`
	H1   int `yaml:"h1"`
	H2   int `yaml:"h2"`
	H3   int `yaml:"h3"`
	H4   int `yaml:"h4"`
}

type ServerConfig struct {
	ID              string     `yaml:"id"`
	DisplayName     string     `yaml:"display_name"`
	Enabled         bool       `yaml:"enabled"`
	Host            string     `yaml:"host"`
	SSHPort         int        `yaml:"ssh_port"`
	SSHUser         string     `yaml:"ssh_user"`
	Interface       string     `yaml:"interface"`
	Endpoint        string     `yaml:"endpoint"`
	ServerPublicKey string     `yaml:"server_public_key"`
	ClientCIDR      string     `yaml:"client_cidr"`
	DNS             []string   `yaml:"dns"`
	AWG             *AWGParams `yaml:"awg"`
}

type RoutesConfig struct {
	URL             string        `yaml:"url"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
}

type MonitorConfig struct {
	CheckInterval time.Duration `yaml:"check_interval"`
	DownThreshold time.Duration `yaml:"down_threshold"`
}

type Config struct {
	AdminIDs     []int64        `yaml:"admin_ids"`
	DefaultLimit int            `yaml:"default_limit"`
	Routes       RoutesConfig   `yaml:"routes"`
	Monitor      MonitorConfig  `yaml:"monitor"`
	Servers      []ServerConfig `yaml:"servers"`

	BotToken      string `yaml:"-"`
	DatabaseURL   string `yaml:"-"`
	SSHPrivateKey string `yaml:"-"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	c.BotToken = os.Getenv("BOT_TOKEN")
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.SSHPrivateKey = os.Getenv("SSH_PRIVATE_KEY")
	if v := os.Getenv("ADMIN_IDS"); v != "" {
		ids := make([]int64, 0, 4)
		for _, part := range strings.Split(v, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("parse ADMIN_IDS %q: %w", part, err)
			}
			ids = append(ids, id)
		}
		c.AdminIDs = ids
	}
	c.setDefaults()
	return c, c.Validate()
}

func (c *Config) setDefaults() {
	for i := range c.Servers {
		if c.Servers[i].SSHPort == 0 {
			c.Servers[i].SSHPort = 22
		}
		if c.Servers[i].Interface == "" {
			c.Servers[i].Interface = "wg0"
		}
	}
	if c.Monitor.CheckInterval == 0 {
		c.Monitor.CheckInterval = 30 * time.Second
	}
	if c.Monitor.DownThreshold == 0 {
		c.Monitor.DownThreshold = 2 * time.Minute
	}
	if c.Routes.RefreshInterval == 0 {
		c.Routes.RefreshInterval = time.Hour
	}
}

func (c Config) Validate() error {
	if c.BotToken == "" {
		return fmt.Errorf("env BOT_TOKEN is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("env DATABASE_URL is required")
	}
	if c.SSHPrivateKey == "" {
		return fmt.Errorf("env SSH_PRIVATE_KEY is required")
	}
	if len(c.AdminIDs) == 0 {
		return fmt.Errorf("admin_ids (or ADMIN_IDS env) must not be empty")
	}
	if c.DefaultLimit <= 0 {
		return fmt.Errorf("default_limit must be positive")
	}
	seen := map[string]bool{}
	enabled := 0
	for _, s := range c.Servers {
		if s.ID == "" {
			return fmt.Errorf("server id must not be empty")
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate server id %q", s.ID)
		}
		seen[s.ID] = true
		if !s.Enabled {
			continue
		}
		enabled++
		if s.Host == "" || s.SSHUser == "" {
			return fmt.Errorf("server %q: host and ssh_user are required", s.ID)
		}
		if s.Endpoint == "" {
			return fmt.Errorf("server %q: endpoint is required", s.ID)
		}
		if s.ServerPublicKey == "" {
			return fmt.Errorf("server %q: server_public_key is required", s.ID)
		}
		if _, _, err := net.ParseCIDR(s.ClientCIDR); err != nil {
			return fmt.Errorf("server %q: bad client_cidr: %w", s.ID, err)
		}
	}
	if enabled == 0 {
		return fmt.Errorf("at least one enabled server is required")
	}
	return nil
}

func (c Config) EnabledServers() []ServerConfig {
	var out []ServerConfig
	for _, s := range c.Servers {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

func (c Config) ServerByID(id string) (ServerConfig, bool) {
	for _, s := range c.Servers {
		if s.ID == id {
			return s, true
		}
	}
	return ServerConfig{}, false
}

func (c Config) DefaultServer() (ServerConfig, error) {
	if ss := c.EnabledServers(); len(ss) > 0 {
		return ss[0], nil
	}
	return ServerConfig{}, fmt.Errorf("no enabled servers configured")
}
