# Amnezia Bot MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Telegram-бот на Go, через который доверенные пользователи сами создают и удаляют конфиги AmneziaWG с split-routing, состоянием в PostgreSQL и алертами администраторам.

**Architecture:** Слои: Telegram-бот (telegram-bot-api v5) → Application service (доступ, лимиты, сценарии) → VPN provider (SSH, sudo-скрипты на сервере) + Config patcher + Routes service (last-known-good список AllowedIPs). Приложение stateless, всё постоянное состояние — в PostgreSQL (pgx/v5, миграции goose, embedded). Одна реплика.

**Tech Stack:** Go 1.23, `github.com/go-telegram-bot-api/telegram-bot-api/v5`, `github.com/jackc/pgx/v5`, `github.com/pressly/goose/v3`, `golang.org/x/crypto` (ssh, curve25519), `gopkg.in/yaml.v3`, stdlib `log/slog`.

**Spec:** `docs/amnezia-manager-bot_spec.md`

## Global Constraints

- Модуль Go: `amnezia-manager-bot`; Go 1.23; только перечисленные зависимости (никаких ORM/фреймворков).
- Доступ определяется только числовым Telegram User ID, не по username (спека §9).
- В БД и логах отсутствуют private keys и полные VPN-конфиги (спека §9, AC-10). В `vpn_peers` хранится только публичный ключ.
- Ошибки пользователю — без внутренних адресов, токенов и технических деталей (спека §9).
- IPv6 не поддерживается: в клиентском конфиге нет IPv6-строк; список AllowedIPs валидируется как «только IPv4» (спека §7). Блокировку IPv6-обхода туннеля выполняет firewall сервера (deploy/server/README.md).
- Секреты (BOT_TOKEN, DATABASE_URL, путь к SSH-ключу) — только через env; серверы/админы/лимиты — в YAML (спека §5).
- Управление VPN-сервером — SSH от отдельного пользователя, только sudo-скрипты `awg-peer-add`, `awg-peer-remove`, `awg-health` (спека §9).
- `go test ./...` проходит без внешних сервисов; интеграционные тесты Postgres — только при `TEST_POSTGRES=1`.
- Алерты: одно статусное сообщение на (сервер, админ), все обновления редактируют его; новое сообщение — только если его ещё нет или edit упал (спека §8, AC-11).
- Коммиты: conventional commits (`feat:`, `test:`, `chore:`, `docs:`), английский.
- Split-routing источник: `https://raw.githubusercontent.com/w1zardz/amnezia-split-route-sync/master/dist/wg-allowed-ips.txt` (одна строка `AllowedIPs = cidr, cidr, ...`, ~66KB).
- Задачи выполнять строго по порядку — каждая следующая использует интерфейсы из предыдущих.

## File Structure

```
cmd/bot/main.go                       — точка входа, wiring, /healthz, graceful shutdown
configs/config.example.yaml           — пример конфига (ConfigMap)
internal/config/config.go             — загрузка/валидация YAML + env-секреты
internal/db/db.go                     — pgxpool + goose embedded migrations
internal/db/migrations/0001_init.sql  — схема БД
internal/store/store.go               — типы + интерфейс Store + ErrNotFound
internal/store/memory/memory.go       — in-memory Store (тесты)
internal/store/postgres/postgres.go   — Postgres Store
internal/vpn/provider.go              — интерфейс Provider + BuildClientConfig
internal/vpn/keys.go                  — генерация пары ключей X25519
internal/vpn/sshprovider/provider.go  — SSH-реализация Provider
internal/patcher/patcher.go           — замена AllowedIPs + валидация списка
internal/routes/routes.go             — last-known-good список AllowedIPs
internal/routes/assets/allowed_ips_default.txt — встроенный начальный список
internal/netalloc/alloc.go            — аллокация клиентских IP в подсети
internal/service/service.go           — бизнес-логика (пользователь + админ)
internal/alerts/alerts.go             — статусные карточки серверов для админов
internal/monitor/monitor.go           — периодическая проверка серверов
internal/tgbot/bot.go                 — роутинг апдейтов, Sender
internal/tgbot/handlers_user.go       — пользовательские сценарии
internal/tgbot/handlers_admin.go      — админ-команды
internal/tgbot/state.go               — состояния диалогов
internal/tgbot/texts.go               — тексты сообщений и маппинг ошибок
deploy/docker/Dockerfile
deploy/k8s/{deployment.yaml,configmap.yaml,secret.example.yaml}
deploy/server/{awg-peer-add,awg-peer-remove,awg-health,test_scripts.sh,amnezia-bot.sudoers,README.md}
Makefile, README.md, .gitignore, go.mod
```

---

### Task 1: Скелет модуля + конфигурация

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`, `README.md`, `configs/config.example.yaml`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: нет (первая задача).
- Produces: `config.Config`, `config.ServerConfig`, `config.Load(path) (Config, error)`, `Config.Validate() error`, `Config.EnabledServers() []ServerConfig`, `Config.ServerByID(id) (ServerConfig, bool)`, `Config.DefaultServer() (ServerConfig, error)`; поля `BotToken`, `DatabaseURL`, `SSHPrivateKey` (из env). Используют задачи 8, 10, 11, 13, 15.

- [ ] **Step 1: Инициализация модуля**

```bash
go mod init amnezia-manager-bot
go get gopkg.in/yaml.v3
mkdir -p internal/config configs
```

`.gitignore`:
```
bin/
.env
*.local.yaml
```

`README.md`:
```markdown
# amnezia-manager-bot

Telegram-бот для самостоятельной выдачи конфигов AmneziaWG доверенным пользователям.
Спека: docs/amnezia-manager-bot_spec.md

## Разработка
    make test              # unit-тесты (без внешних сервисов)
    make test-integration  # + Postgres в docker
    make lint              # golangci-lint
    make build             # bin/amnezia-bot

## Запуск
    BOT_TOKEN=... DATABASE_URL=... SSH_PRIVATE_KEY=/path/to/key \
      ./bin/amnezia-bot -config configs/config.yaml
```

- [ ] **Step 2: Пример конфига**

`configs/config.example.yaml`:
```yaml
admin_ids: [111111111]
default_limit: 3

routes:
  url: https://raw.githubusercontent.com/w1zardz/amnezia-split-route-sync/master/dist/wg-allowed-ips.txt
  refresh_interval: 1h

monitor:
  check_interval: 30s
  down_threshold: 2m

servers:
  - id: spb-1
    display_name: SPB VPN-1
    enabled: true
    host: 203.0.113.10
    ssh_port: 22
    ssh_user: amnezia-bot
    interface: wg0
    endpoint: 203.0.113.10:51820
    server_public_key: "BASE64_PUBKEY_OF_SERVER"
    client_cidr: 10.8.1.0/24
    dns: []
    awg:
      jc: 4
      jmin: 40
      jmax: 70
      s1: 68
      s2: 149
      h1: 1234567
      h2: 2345678
      h3: 3456789
      h4: 4567890
```

- [ ] **Step 3: Тесты конфига**

`internal/config/config_test.go`:
```go
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
		"bad cidr":    validYAML + "\n  - id: s2\n    display_name: X\n    endpoint: 1.2.3.4:5\n    server_public_key: B\n    client_cidr: nope\n",
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
```

- [ ] **Step 4: Запустить тесты — должны упасть**

Run: `go test ./internal/config/`
Expected: FAIL — undefined: Load

- [ ] **Step 5: Реализация конфига**

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type AWGParams struct {
	Jc, Jmin, Jmax, S1, S2, H1, H2, H3, H4 int `yaml:",inline"`
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
		return fmt.Errorf("admin_ids must not be empty")
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
```

- [ ] **Step 6: Тесты проходят + Makefile + коммит**

`Makefile`:
```make
GO ?= go
BIN := bin/amnezia-bot

.PHONY: build test test-integration lint routes-download docker

build:
	$(GO) build -o $(BIN) ./cmd/bot

test:
	$(GO) test ./...

test-integration:
	docker run -d --rm --name amnezia-test-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=amnezia_test -p 54329:5432 postgres:16-alpine || true
	until docker exec amnezia-test-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
	TEST_POSTGRES=1 TEST_DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_test?sslmode=disable" $(GO) test ./...; \
	status=$$?; docker stop amnezia-test-pg; exit $$status

lint:
	golangci-lint run

routes-download:
	curl -fsSL "$(URL)" -o internal/routes/assets/allowed_ips_default.txt

docker:
	docker build -f deploy/docker/Dockerfile -t amnezia-bot:dev .
```

Run: `go test ./internal/config/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: module skeleton and config loading"
```

---
### Task 2: Миграции и подключение к PostgreSQL

**Files:**
- Create: `internal/db/db.go`, `internal/db/migrations/0001_init.sql`

**Interfaces:**
- Consumes: `config.Config.DatabaseURL`.
- Produces: `db.Connect(ctx, url) (*pgxpool.Pool, error)`, `db.Migrate(pool) error`; таблицы `users`, `user_server_access`, `vpn_peers`, `server_status_messages`. Используют задачи 4, 15.

Примечание: корректность миграций проверяется интеграционными тестами в Task 4; здесь — компиляция и сборка.

- [ ] **Step 1: Зависимости и директория**

```bash
go get github.com/jackc/pgx/v5 github.com/pressly/goose/v3
mkdir -p internal/db/migrations
```

- [ ] **Step 2: Миграция**

`internal/db/migrations/0001_init.sql`:
```sql
-- +goose Up
CREATE TABLE users (
    telegram_id  BIGINT PRIMARY KEY,
    username     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    config_limit INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_server_access (
    telegram_id BIGINT NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    server_id   TEXT NOT NULL,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (telegram_id, server_id)
);

CREATE TABLE vpn_peers (
    id          BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL REFERENCES users (telegram_id),
    server_id   TEXT NOT NULL,
    peer_id     TEXT NOT NULL,
    device_name TEXT NOT NULL,
    client_ip   INET NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ,
    UNIQUE (server_id, peer_id),
    UNIQUE (server_id, client_ip)
);
CREATE INDEX idx_vpn_peers_active ON vpn_peers (telegram_id) WHERE revoked_at IS NULL;

CREATE TABLE server_status_messages (
    server_id  TEXT NOT NULL,
    admin_id   BIGINT NOT NULL,
    chat_id    BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, admin_id)
);

-- +goose Down
DROP TABLE server_status_messages;
DROP TABLE vpn_peers;
DROP TABLE user_server_access;
DROP TABLE users;
```

- [ ] **Step 3: Подключение и запуск миграций**

`internal/db/db.go`:
```go
package db

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func Migrate(pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	// sqlDB не закрываем: он разделяет пул с приложением.
	sqlDB := stdlib.OpenDBFromPool(pool)
	return goose.Up(sqlDB, "migrations")
}
```

- [ ] **Step 4: Сборка + коммит**

Run: `go build ./... && go vet ./...`
Expected: OK

```bash
git add -A && git commit -m "feat: postgres connection and goose migrations"
```

---

### Task 3: Контракт Store + in-memory реализация

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/memory/memory.go`
- Test: `internal/store/memory/memory_test.go`

**Interfaces:**
- Consumes: нет.
- Produces: интерфейс `store.Store` (полный код ниже), типы `store.User`, `store.Peer`, `store.StatusMessage`, `store.ErrNotFound`; `memory.New() *MemoryStore`. Используют задачи 4, 10, 11, 12, 15.

- [ ] **Step 1: Тесты memory-store**

`internal/store/memory/memory_test.go`:
```go
package memory

import (
	"context"
	"errors"
	"testing"

	"amnezia-manager-bot/internal/store"
)

func TestUserCRUD(t *testing.T) {
	m := New()
	ctx := context.Background()
	if _, err := m.GetUser(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := m.UpsertUser(ctx, store.User{TelegramID: 1, Username: "u", Enabled: true, ConfigLimit: 3}); err != nil {
		t.Fatal(err)
	}
	u, err := m.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 3 {
		t.Fatalf("get: %v %+v", err, u)
	}
	if err := m.SetUserLimit(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.ConfigLimit != 5 {
		t.Fatal("limit not updated")
	}
	if err := m.SetUserEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.Enabled {
		t.Fatal("not disabled")
	}
	if err := m.SetUsername(ctx, 1, "new"); err != nil {
		t.Fatal(err)
	}
	if u, _ := m.GetUser(ctx, 1); u.Username != "new" {
		t.Fatal("username not updated")
	}
	if err := m.SetUserEnabled(ctx, 42, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAccess(t *testing.T) {
	m := New()
	ctx := context.Background()
	_ = m.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 1})
	if ok, _ := m.HasAccess(ctx, 1, "s1"); ok {
		t.Fatal("unexpected access")
	}
	if err := m.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.HasAccess(ctx, 1, "s1"); !ok {
		t.Fatal("no access after grant")
	}
	srvs, err := m.ListUserServers(ctx, 1)
	if err != nil || len(srvs) != 1 || srvs[0] != "s1" {
		t.Fatalf("ListUserServers: %v %v", err, srvs)
	}
}

func TestPeers(t *testing.T) {
	m := New()
	ctx := context.Background()
	_ = m.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 2})
	p, err := m.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB", DeviceName: "phone", ClientIP: "10.8.1.2"})
	if err != nil || p.ID == 0 || p.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, p)
	}
	got, err := m.GetActivePeer(ctx, 1, p.ID)
	if err != nil || got.DeviceName != "phone" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := m.GetActivePeer(ctx, 2, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign peer must be not found, got %v", err)
	}
	if n := len(mustPeers(t, m, 1)); n != 1 {
		t.Fatalf("active = %d", n)
	}
	if err := m.RevokePeer(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(mustPeers(t, m, 1)); n != 0 {
		t.Fatalf("active after revoke = %d", n)
	}
	if _, err := m.GetActivePeer(ctx, 1, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked peer must be not found, got %v", err)
	}
	onServer, err := m.ListActivePeersOnServer(ctx, "s1")
	if err != nil || len(onServer) != 0 {
		t.Fatalf("ListActivePeersOnServer: %v %d", err, len(onServer))
	}
	all, err := m.ListActivePeersAll(ctx)
	if err != nil || len(all) != 0 {
		t.Fatalf("ListActivePeersAll: %v %d", err, len(all))
	}
}

func mustPeers(t *testing.T, m *MemoryStore, uid int64) []store.Peer {
	t.Helper()
	ps, err := m.ListActivePeers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestStatusMessages(t *testing.T) {
	m := New()
	ctx := context.Background()
	if _, err := m.GetStatusMessage(ctx, "s1", 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := m.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 100}); err != nil {
		t.Fatal(err)
	}
	sm, err := m.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID != 100 {
		t.Fatalf("get: %v %+v", err, sm)
	}
	if err := m.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 200}); err != nil {
		t.Fatal(err)
	}
	if sm, _ = m.GetStatusMessage(ctx, "s1", 10); sm.MessageID != 200 {
		t.Fatal("upsert failed")
	}
}
```

- [ ] **Step 2: Запустить тесты — упадут**

Run: `go test ./internal/store/memory/`
Expected: FAIL — пакет не существует

- [ ] **Step 3: Контракт Store**

`internal/store/store.go`:
```go
package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	TelegramID  int64
	Username    string
	Enabled     bool
	ConfigLimit int
}

type Peer struct {
	ID         int64
	TelegramID int64
	ServerID   string
	PeerID     string // публичный ключ клиента; приватный нигде не хранится
	DeviceName string
	ClientIP   string
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

type StatusMessage struct {
	ServerID  string
	AdminID   int64
	ChatID    int64
	MessageID int64
}

type Store interface {
	UpsertUser(ctx context.Context, u User) error
	GetUser(ctx context.Context, telegramID int64) (User, error)
	SetUserEnabled(ctx context.Context, telegramID int64, enabled bool) error
	SetUserLimit(ctx context.Context, telegramID int64, limit int) error
	SetUsername(ctx context.Context, telegramID int64, username string) error
	ListUsers(ctx context.Context) ([]User, error)

	GrantAccess(ctx context.Context, telegramID int64, serverID string) error
	HasAccess(ctx context.Context, telegramID int64, serverID string) (bool, error)
	ListUserServers(ctx context.Context, telegramID int64) ([]string, error)

	CreatePeer(ctx context.Context, p Peer) (Peer, error)
	ListActivePeers(ctx context.Context, telegramID int64) ([]Peer, error)
	ListActivePeersOnServer(ctx context.Context, serverID string) ([]Peer, error)
	ListActivePeersAll(ctx context.Context) ([]Peer, error)
	GetActivePeer(ctx context.Context, telegramID int64, id int64) (Peer, error)
	RevokePeer(ctx context.Context, id int64) error

	GetStatusMessage(ctx context.Context, serverID string, adminID int64) (StatusMessage, error)
	SaveStatusMessage(ctx context.Context, m StatusMessage) error
}
```

- [ ] **Step 4: Реализация memory-store**

`internal/store/memory/memory.go`:
```go
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"amnezia-manager-bot/internal/store"
)

type MemoryStore struct {
	mu         sync.Mutex
	users      map[int64]store.User
	access     map[int64]map[string]bool
	peers      map[int64]store.Peer
	statusMsgs map[string]map[int64]store.StatusMessage
	nextPeerID int64
}

func New() *MemoryStore {
	return &MemoryStore{
		users:      map[int64]store.User{},
		access:     map[int64]map[string]bool{},
		peers:      map[int64]store.Peer{},
		statusMsgs: map[string]map[int64]store.StatusMessage{},
		nextPeerID: 1,
	}
}

func (m *MemoryStore) UpsertUser(_ context.Context, u store.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[u.TelegramID] = u
	return nil
}

func (m *MemoryStore) GetUser(_ context.Context, id int64) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (m *MemoryStore) SetUserEnabled(_ context.Context, id int64, enabled bool) error {
	return m.updateUser(id, func(u *store.User) { u.Enabled = enabled })
}

func (m *MemoryStore) SetUserLimit(_ context.Context, id int64, limit int) error {
	return m.updateUser(id, func(u *store.User) { u.ConfigLimit = limit })
}

func (m *MemoryStore) SetUsername(_ context.Context, id int64, username string) error {
	return m.updateUser(id, func(u *store.User) { u.Username = username })
}

func (m *MemoryStore) updateUser(id int64, f func(*store.User)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.ErrNotFound
	}
	f(&u)
	m.users[id] = u
	return nil
}

func (m *MemoryStore) ListUsers(_ context.Context) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TelegramID < out[j].TelegramID })
	return out, nil
}

func (m *MemoryStore) GrantAccess(_ context.Context, telegramID int64, serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.access[telegramID] == nil {
		m.access[telegramID] = map[string]bool{}
	}
	m.access[telegramID][serverID] = true
	return nil
}

func (m *MemoryStore) HasAccess(_ context.Context, telegramID int64, serverID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.access[telegramID][serverID], nil
}

func (m *MemoryStore) ListUserServers(_ context.Context, telegramID int64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id := range m.access[telegramID] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) CreatePeer(_ context.Context, p store.Peer) (store.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.ID = m.nextPeerID
	m.nextPeerID++
	p.CreatedAt = time.Now().UTC()
	m.peers[p.ID] = p
	return p, nil
}

func (m *MemoryStore) ListActivePeers(_ context.Context, telegramID int64) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil && p.TelegramID == telegramID }), nil
}

func (m *MemoryStore) ListActivePeersOnServer(_ context.Context, serverID string) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil && p.ServerID == serverID }), nil
}

func (m *MemoryStore) ListActivePeersAll(_ context.Context) ([]store.Peer, error) {
	return m.filterPeers(func(p store.Peer) bool { return p.RevokedAt == nil }), nil
}

func (m *MemoryStore) filterPeers(f func(store.Peer) bool) []store.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Peer
	for _, p := range m.peers {
		if f(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *MemoryStore) GetActivePeer(_ context.Context, telegramID int64, id int64) (store.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok || p.RevokedAt != nil || p.TelegramID != telegramID {
		return store.Peer{}, store.ErrNotFound
	}
	return p, nil
}

func (m *MemoryStore) RevokePeer(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	p.RevokedAt = &now
	m.peers[id] = p
	return nil
}

func (m *MemoryStore) GetStatusMessage(_ context.Context, serverID string, adminID int64) (store.StatusMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.statusMsgs[serverID][adminID]
	if !ok {
		return store.StatusMessage{}, store.ErrNotFound
	}
	return sm, nil
}

func (m *MemoryStore) SaveStatusMessage(_ context.Context, sm store.StatusMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusMsgs[sm.ServerID] == nil {
		m.statusMsgs[sm.ServerID] = map[int64]store.StatusMessage{}
	}
	m.statusMsgs[sm.ServerID][sm.AdminID] = sm
	return nil
}
```

- [ ] **Step 5: Тесты проходят + коммит**

Run: `go test ./internal/store/... -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: store contract and in-memory implementation"
```

---
### Task 4: Postgres-реализация Store + интеграционные тесты

**Files:**
- Create: `internal/store/postgres/postgres.go`
- Test: `internal/store/postgres/postgres_test.go` (интеграционный, `TEST_POSTGRES=1`)

**Interfaces:**
- Consumes: `store.Store`, `db.Connect`, `db.Migrate`.
- Produces: `postgres.New(pool *pgxpool.Pool) *Store` — реализует `store.Store`. Использует задача 15.

- [ ] **Step 1: Интеграционный тест**

`internal/store/postgres/postgres_test.go`:
```go
package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"amnezia-manager-bot/internal/db"
	"amnezia-manager-bot/internal/store"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("set TEST_POSTGRES=1 to run integration tests")
	}
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:54329/amnezia_test?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, user_server_access, vpn_peers, server_status_messages RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return New(pool)
}

func TestUserLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.UpsertUser(ctx, store.User{TelegramID: 1, Username: "u", Enabled: true, ConfigLimit: 3}); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 3 {
		t.Fatalf("get: %v %+v", err, u)
	}
	if err := s.SetUserLimit(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUsername(ctx, 1, "newname"); err != nil {
		t.Fatal(err)
	}
	u, err = s.GetUser(ctx, 1)
	if err != nil || u.ConfigLimit != 7 || u.Enabled || u.Username != "newname" {
		t.Fatalf("updated: %v %+v", err, u)
	}
	users, err := s.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("list: %v %d", err, len(users))
	}
	if err := s.SetUserEnabled(ctx, 99, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAccess(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = s.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 1})
	if ok, _ := s.HasAccess(ctx, 1, "s1"); ok {
		t.Fatal("unexpected access")
	}
	if err := s.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.GrantAccess(ctx, 1, "s1"); err != nil {
		t.Fatalf("idempotent grant: %v", err)
	}
	if ok, _ := s.HasAccess(ctx, 1, "s1"); !ok {
		t.Fatal("no access after grant")
	}
	srvs, err := s.ListUserServers(ctx, 1)
	if err != nil || len(srvs) != 1 || srvs[0] != "s1" {
		t.Fatalf("ListUserServers: %v %v", err, srvs)
	}
}

func TestPeers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = s.UpsertUser(ctx, store.User{TelegramID: 1, Enabled: true, ConfigLimit: 3})
	p, err := s.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB1", DeviceName: "phone", ClientIP: "10.8.1.2"})
	if err != nil || p.ID == 0 || p.CreatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, p)
	}
	if _, err := s.CreatePeer(ctx, store.Peer{TelegramID: 1, ServerID: "s1", PeerID: "PUB2", DeviceName: "pc", ClientIP: "10.8.1.2"}); err == nil {
		t.Fatal("duplicate client_ip must fail (unique constraint)")
	}
	got, err := s.GetActivePeer(ctx, 1, p.ID)
	if err != nil || got.ClientIP != "10.8.1.2" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := s.GetActivePeer(ctx, 2, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign peer must be ErrNotFound, got %v", err)
	}
	if n := len(listActive(t, s, 1)); n != 1 {
		t.Fatalf("active = %d", n)
	}
	if err := s.RevokePeer(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(listActive(t, s, 1)); n != 0 {
		t.Fatalf("active after revoke = %d", n)
	}
	onServer, err := s.ListActivePeersOnServer(ctx, "s1")
	if err != nil || len(onServer) != 0 {
		t.Fatalf("on server: %v %d", err, len(onServer))
	}
}

func listActive(t *testing.T, s *Store, uid int64) []store.Peer {
	t.Helper()
	ps, err := s.ListActivePeers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestStatusMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.GetStatusMessage(ctx, "s1", 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveStatusMessage(ctx, store.StatusMessage{ServerID: "s1", AdminID: 10, ChatID: 10, MessageID: 200}); err != nil {
		t.Fatal(err)
	}
	sm, err := s.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID != 200 {
		t.Fatalf("get: %v %+v", err, sm)
	}
}
```

- [ ] **Step 2: Запустить без Postgres — пропуск**

Run: `go test ./internal/store/postgres/ -v`
Expected: SKIP

- [ ] **Step 3: Реализация**

`internal/store/postgres/postgres.go`:
```go
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"amnezia-manager-bot/internal/store"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (store.User, error) {
	var u store.User
	err := r.Scan(&u.TelegramID, &u.Username, &u.Enabled, &u.ConfigLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, store.ErrNotFound
	}
	return u, err
}

const peerCols = "id, telegram_id, server_id, peer_id, device_name, host(client_ip), created_at, revoked_at"

func scanPeer(r scanner) (store.Peer, error) {
	var p store.Peer
	err := r.Scan(&p.ID, &p.TelegramID, &p.ServerID, &p.PeerID, &p.DeviceName, &p.ClientIP, &p.CreatedAt, &p.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, store.ErrNotFound
	}
	return p, err
}

func (s *Store) UpsertUser(ctx context.Context, u store.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (telegram_id, username, enabled, config_limit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE
		SET username = EXCLUDED.username, enabled = EXCLUDED.enabled, config_limit = EXCLUDED.config_limit`,
		u.TelegramID, u.Username, u.Enabled, u.ConfigLimit)
	return err
}

func (s *Store) GetUser(ctx context.Context, telegramID int64) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		"SELECT telegram_id, username, enabled, config_limit FROM users WHERE telegram_id = $1", telegramID))
}

func (s *Store) SetUserEnabled(ctx context.Context, telegramID int64, enabled bool) error {
	return s.execUserUpdate(ctx, "UPDATE users SET enabled = $2 WHERE telegram_id = $1", telegramID, enabled)
}

func (s *Store) SetUserLimit(ctx context.Context, telegramID int64, limit int) error {
	return s.execUserUpdate(ctx, "UPDATE users SET config_limit = $2 WHERE telegram_id = $1", telegramID, limit)
}

func (s *Store) SetUsername(ctx context.Context, telegramID int64, username string) error {
	return s.execUserUpdate(ctx, "UPDATE users SET username = $2 WHERE telegram_id = $1", telegramID, username)
}

func (s *Store) execUserUpdate(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, "SELECT telegram_id, username, enabled, config_limit FROM users ORDER BY telegram_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GrantAccess(ctx context.Context, telegramID int64, serverID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_server_access (telegram_id, server_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, telegramID, serverID)
	return err
}

func (s *Store) HasAccess(ctx context.Context, telegramID int64, serverID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM user_server_access WHERE telegram_id = $1 AND server_id = $2)",
		telegramID, serverID).Scan(&ok)
	return ok, err
}

func (s *Store) ListUserServers(ctx context.Context, telegramID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT server_id FROM user_server_access WHERE telegram_id = $1 ORDER BY server_id", telegramID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) CreatePeer(ctx context.Context, p store.Peer) (store.Peer, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vpn_peers (telegram_id, server_id, peer_id, device_name, client_ip)
		VALUES ($1, $2, $3, $4, $5::inet)
		RETURNING id, created_at`,
		p.TelegramID, p.ServerID, p.PeerID, p.DeviceName, p.ClientIP).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return p, fmt.Errorf("insert peer: %w", err)
	}
	return p, nil
}

func (s *Store) listPeers(ctx context.Context, sql string, args ...any) ([]store.Peer, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Peer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListActivePeers(ctx context.Context, telegramID int64) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE telegram_id = $1 AND revoked_at IS NULL ORDER BY id", telegramID)
}

func (s *Store) ListActivePeersOnServer(ctx context.Context, serverID string) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE server_id = $1 AND revoked_at IS NULL ORDER BY id", serverID)
}

func (s *Store) ListActivePeersAll(ctx context.Context) ([]store.Peer, error) {
	return s.listPeers(ctx, "SELECT "+peerCols+" FROM vpn_peers WHERE revoked_at IS NULL ORDER BY id")
}

func (s *Store) GetActivePeer(ctx context.Context, telegramID int64, id int64) (store.Peer, error) {
	return scanPeer(s.pool.QueryRow(ctx,
		"SELECT "+peerCols+" FROM vpn_peers WHERE id = $1 AND telegram_id = $2 AND revoked_at IS NULL", id, telegramID))
}

func (s *Store) RevokePeer(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, "UPDATE vpn_peers SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetStatusMessage(ctx context.Context, serverID string, adminID int64) (store.StatusMessage, error) {
	var sm store.StatusMessage
	err := s.pool.QueryRow(ctx,
		"SELECT server_id, admin_id, chat_id, message_id FROM server_status_messages WHERE server_id = $1 AND admin_id = $2",
		serverID, adminID).Scan(&sm.ServerID, &sm.AdminID, &sm.ChatID, &sm.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sm, store.ErrNotFound
	}
	return sm, err
}

func (s *Store) SaveStatusMessage(ctx context.Context, sm store.StatusMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO server_status_messages (server_id, admin_id, chat_id, message_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id, admin_id) DO UPDATE
		SET chat_id = EXCLUDED.chat_id, message_id = EXCLUDED.message_id, updated_at = now()`,
		sm.ServerID, sm.AdminID, sm.ChatID, sm.MessageID)
	return err
}
```

- [ ] **Step 4: Интеграционный прогон**

Run: `make test-integration`
Expected: все тесты PASS (нужен запущенный Docker)

- [ ] **Step 5: Коммит**

```bash
git add -A && git commit -m "feat: postgres store with integration tests"
```

---
### Task 5: Генерация ключей WireGuard/AmneziaWG

**Files:**
- Create: `internal/vpn/keys.go`
- Test: `internal/vpn/keys_test.go`

**Interfaces:**
- Consumes: нет.
- Produces: `vpn.GenerateKeyPair() (privateKey, publicKey string, err error)` — base64 X25519. Использует задача 10.

- [ ] **Step 1: Тест**

`internal/vpn/keys_test.go`:
```go
package vpn

import (
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := base64.StdEncoding.DecodeString(priv)
	if err != nil || len(privBytes) != 32 {
		t.Fatalf("priv key: %v len=%d", err, len(privBytes))
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || len(pubBytes) != 32 {
		t.Fatalf("pub key: %v len=%d", err, len(pubBytes))
	}
	derived, err := curve25519.X25519(privBytes, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	if string(derived) != string(pubBytes) {
		t.Fatal("public key does not match private key")
	}
	if priv == pub {
		t.Fatal("keys must differ")
	}
}
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/vpn/ -run TestGenerateKeyPair`
Expected: FAIL — undefined: GenerateKeyPair

- [ ] **Step 3: Реализация**

```bash
go get golang.org/x/crypto
```

`internal/vpn/keys.go`:
```go
package vpn

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeyPair генерирует пару ключей X25519 (формат WireGuard/AmneziaWG, base64).
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("read random: %w", err)
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("x25519: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub), nil
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/vpn/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: wireguard keypair generation"
```

---

### Task 6: Config patcher (AllowedIPs + валидация списка)

**Files:**
- Create: `internal/patcher/patcher.go`
- Test: `internal/patcher/patcher_test.go`

**Interfaces:**
- Consumes: нет.
- Produces: `patcher.Patch(config string, cidrs []string) (string, error)`, `patcher.ValidateAndClean(cidrs []string) ([]string, error)`, `patcher.ErrNoAllowedIPs`. Используют задачи 7, 10.

- [ ] **Step 1: Тесты**

`internal/patcher/patcher_test.go`:
```go
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
		"zero route": {"0.0.0.0/0"},
		"private":    {"1.0.0.0/8", "10.0.0.0/8"},
		"172 private": {"172.16.0.0/12"},
		"192 private": {"192.168.0.0/16"},
		"loopback":   {"1.0.0.0/8", "127.0.0.0/8"},
		"link-local": {"169.254.0.0/16"},
		"multicast":  {"224.0.0.0/4"},
		"cgnat":      {"100.64.0.0/10"},
		"ipv6":       {"2001:db8::/32"},
		"garbage":    {"not-a-cidr", "1.2.3.4"},
		"empty":      {"", "  "},
		"host bits":  {"1.0.0.1/8"},
	}
	for name, list := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateAndClean(list); err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/patcher/`
Expected: FAIL — пакет пуст

- [ ] **Step 3: Реализация**

`internal/patcher/patcher.go`:
```go
package patcher

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

var ErrNoAllowedIPs = errors.New("AllowedIPs line not found")

// Patch заменяет строки AllowedIPs в конфиге на переданный список CIDR.
func Patch(config string, cidrs []string) (string, error) {
	lines := strings.Split(config, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "AllowedIPs") {
			lines[i] = "AllowedIPs = " + strings.Join(cidrs, ", ")
			found = true
		}
	}
	if !found {
		return "", ErrNoAllowedIPs
	}
	return strings.Join(lines, "\n"), nil
}

var cgnat = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isBlocked(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

// ValidateAndClean проверяет список split-routing CIDR: только IPv4 без хост-битов,
// без приватных/служебных сетей и без 0.0.0.0/0. Возвращает отсортированный
// дедуплицированный список. Любая некорректная запись — ошибка: источник считается
// повреждённым, вызывающий код использует last-known-good.
func ValidateAndClean(cidrs []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ip, n, err := net.ParseCIDR(c)
		if err != nil || n.IP.To4() == nil {
			return nil, fmt.Errorf("invalid ipv4 cidr %q", c)
		}
		if !ip.Equal(n.IP) {
			return nil, fmt.Errorf("cidr %q has host bits set", c)
		}
		if n.String() == "0.0.0.0/0" {
			return nil, errors.New("0.0.0.0/0 is not allowed in split-routing")
		}
		if isBlocked(n.IP) {
			return nil, fmt.Errorf("forbidden network %q", c)
		}
		key := n.String()
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty allowed-ips list")
	}
	sort.Strings(out)
	return out, nil
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/patcher/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: allowedips patcher and list validation"
```

---
### Task 7: Routes service (last-known-good список split-routing)

**Files:**
- Create: `internal/routes/routes.go`
- Create: `internal/routes/assets/allowed_ips_default.txt` (скачивается)
- Test: `internal/routes/routes_test.go`

**Interfaces:**
- Consumes: `patcher.ValidateAndClean`.
- Produces: `routes.New(url string, log *slog.Logger) (*Service, error)`, `(*Service).AllowedIPs() []string`, `(*Service).Refresh(ctx) error`, `(*Service).Run(ctx context.Context, interval time.Duration)`. Используют задачи 10 (через интерфейс), 15.

- [ ] **Step 1: Скачать встроенный список**

```bash
mkdir -p internal/routes/assets
make routes-download URL=https://raw.githubusercontent.com/w1zardz/amnezia-split-route-sync/master/dist/wg-allowed-ips.txt
head -c 80 internal/routes/assets/allowed_ips_default.txt   # должно начинаться с "AllowedIPs = 1.0.0.0/8, ..."
```

- [ ] **Step 2: Тесты**

`internal/routes/routes_test.go`:
```go
package routes

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedDefault(t *testing.T) {
	s, err := New("http://unused", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if list := s.AllowedIPs(); len(list) < 100 {
		t.Fatalf("embedded list too small: %d", len(list))
	}
}

func TestParseFile(t *testing.T) {
	cases := map[string][]string{
		"AllowedIPs = 1.0.0.0/8, 2.0.0.0/7": {"1.0.0.0/8", "2.0.0.0/7"},
		"1.0.0.0/8,2.0.0.0/7":               {"1.0.0.0/8", "2.0.0.0/7"},
		"1.0.0.0/8\n2.0.0.0/7\n":            {"1.0.0.0/8", "2.0.0.0/7"},
	}
	for in, want := range cases {
		if got := parseFile([]byte(in)); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("parseFile(%q) = %v", in, got)
		}
	}
}

func TestRefreshOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("AllowedIPs = 5.0.0.0/8, 6.0.0.0/8"))
	}))
	defer srv.Close()
	s, err := New(srv.URL, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.AllowedIPs(), ","); got != "5.0.0.0/8, 6.0.0.0/8" {
		t.Fatalf("got %q", got)
	}
}

func TestRefreshKeepsLastKnownGood(t *testing.T) {
	s, err := New("http://unused", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Join(s.AllowedIPs(), ",")

	s.url = "http://127.0.0.1:1/unreachable"
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on fetch failure")
	}

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	s.url = srv500.URL
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on 500")
	}

	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("AllowedIPs = 0.0.0.0/0"))
	}))
	defer srvBad.Close()
	s.url = srvBad.URL
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on invalid list")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on invalid list")
	}
}

func TestRunRefreshesPeriodically(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte("AllowedIPs = 5.0.0.0/8"))
	}))
	defer srv.Close()
	s, err := New(srv.URL, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	go s.Run(ctx, 50*time.Millisecond)
	<-ctx.Done()
	if calls < 2 {
		t.Fatalf("expected periodic refreshes, calls=%d", calls)
	}
}
```

- [ ] **Step 3: Упасть**

Run: `go test ./internal/routes/`
Expected: FAIL — пакет пуст

- [ ] **Step 4: Реализация**

`internal/routes/routes.go`:
```go
package routes

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"amnezia-manager-bot/internal/patcher"
)

//go:embed assets/allowed_ips_default.txt
var defaultFS embed.FS

// Service хранит last-known-good список AllowedIPs для split-routing.
// Начальное значение — встроенный файл, далее обновляется из URL.
type Service struct {
	url    string
	client *http.Client
	log    *slog.Logger

	mu   sync.RWMutex
	list []string
}

func New(url string, log *slog.Logger) (*Service, error) {
	raw, err := defaultFS.ReadFile("assets/allowed_ips_default.txt")
	if err != nil {
		return nil, fmt.Errorf("read embedded default list: %w", err)
	}
	list, err := patcher.ValidateAndClean(parseFile(raw))
	if err != nil {
		return nil, fmt.Errorf("embedded default list invalid: %w", err)
	}
	return &Service{
		url:    url,
		client: &http.Client{Timeout: 30 * time.Second},
		log:    log,
		list:   list,
	}, nil
}

func parseFile(b []byte) []string {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "AllowedIPs") {
		if i := strings.IndexByte(s, '='); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
}

// AllowedIPs возвращает текущий last-known-good список (никогда не пустой).
func (s *Service) AllowedIPs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.list...)
}

// Refresh скачивает и валидирует список; при ошибке текущий список не меняется.
func (s *Service) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	list, err := patcher.ValidateAndClean(parseFile(body))
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	s.mu.Lock()
	s.list = list
	s.mu.Unlock()
	return nil
}

// Run периодически обновляет список до отмены ctx.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Refresh(ctx); err != nil {
			s.log.Warn("routes refresh failed, keeping last-known-good", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```

- [ ] **Step 5: Пройти + коммит**

Run: `go test ./internal/routes/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: split-routing list service with last-known-good fallback"
```

---

### Task 8: VPN provider — интерфейс, шаблон конфига, SSH-реализация

**Files:**
- Create: `internal/vpn/provider.go`
- Create: `internal/vpn/sshprovider/provider.go`
- Test: `internal/vpn/provider_test.go`, `internal/vpn/sshprovider/provider_test.go`

**Interfaces:**
- Consumes: `config.Config` (серверы), env `SSH_PRIVATE_KEY` (реальный dial).
- Produces:
  - `vpn.Provider` (interface): `CreatePeer(ctx, serverID, publicKey, clientIP string) error`, `RemovePeer(ctx, serverID, publicKey string) error`, `HealthCheck(ctx, serverID string) error`; `vpn.ErrServerNotFound`.
  - `vpn.BuildClientConfig(srv config.ServerConfig, clientPrivateKey, clientIP string) string`.
  - `sshprovider.New(cfg config.Config, log *slog.Logger) *Provider` — реализует `vpn.Provider`.
  Используют задачи 10, 13, 15.

- [ ] **Step 1: Тест BuildClientConfig**

`internal/vpn/provider_test.go`:
```go
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
```

- [ ] **Step 2: Интерфейс + шаблон, тест зелёный**

`internal/vpn/provider.go`:
```go
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
```

Run: `go test ./internal/vpn/ -run TestBuild -v`
Expected: PASS

- [ ] **Step 3: Тест SSH-провайдера (fake client)**

`internal/vpn/sshprovider/provider_test.go`:
```go
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
```

- [ ] **Step 4: Упасть**

Run: `go test ./internal/vpn/sshprovider/`
Expected: FAIL — пакет пуст

- [ ] **Step 5: Реализация SSH-провайдера**

`internal/vpn/sshprovider/provider.go`:
```go
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
	defer sess.Close()
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
```

- [ ] **Step 6: Пройти + коммит**

Run: `go test ./internal/vpn/... -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: ssh vpn provider and client config template"
```

---
### Task 9: Аллокация клиентских IP

**Files:**
- Create: `internal/netalloc/alloc.go`
- Test: `internal/netalloc/alloc_test.go`

**Interfaces:**
- Consumes: нет.
- Produces: `netalloc.Allocate(cidr *net.IPNet, used []net.IP) (net.IP, error)`, `netalloc.ErrNoFreeIPs`. Использует задача 10.

- [ ] **Step 1: Тест**

`internal/netalloc/alloc_test.go`:
```go
package netalloc

import (
	"errors"
	"net"
	"strconv"
	"testing"
)

func ip(s string) net.IP { return net.ParseIP(s) }

func netw(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestAllocate(t *testing.T) {
	got, err := Allocate(netw("10.8.1.0/24"), nil)
	if err != nil || got.String() != "10.8.1.2" {
		t.Fatalf("got %v %v", got, err)
	}
	got, err = Allocate(netw("10.8.1.0/24"), []net.IP{ip("10.8.1.2"), ip("10.8.1.3")})
	if err != nil || got.String() != "10.8.1.4" {
		t.Fatalf("got %v %v", got, err)
	}
	// .0 (сеть) и .1 (резерв VPN-сервера) никогда не выдаются
	got, err = Allocate(netw("10.8.1.0/24"), []net.IP{ip("10.8.1.0"), ip("10.8.1.1")})
	if err != nil || got.String() != "10.8.1.2" {
		t.Fatalf("got %v %v", got, err)
	}
	// несмежные занятые
	got, err = Allocate(netw("10.9.0.0/24"), []net.IP{ip("10.9.0.2"), ip("10.9.0.5")})
	if err != nil || got.String() != "10.9.0.3" {
		t.Fatalf("got %v %v", got, err)
	}
	// полный диапазон: последний валидный .254, broadcast .255 недоступен
	var used []net.IP
	for i := 2; i <= 254; i++ {
		used = append(used, ip("10.8.1."+strconv.Itoa(i)))
	}
	if _, err := Allocate(netw("10.8.1.0/24"), used); !errors.Is(err, ErrNoFreeIPs) {
		t.Fatalf("want ErrNoFreeIPs, got %v", err)
	}
	got, err = Allocate(netw("10.8.1.0/24"), used[:len(used)-1])
	if err != nil || got.String() != "10.8.1.254" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAllocateIPv6Rejected(t *testing.T) {
	if _, err := Allocate(netw("fd00::/64"), nil); err == nil {
		t.Fatal("ipv6 must be rejected")
	}
}
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/netalloc/`
Expected: FAIL — пакет пуст

- [ ] **Step 3: Реализация**

`internal/netalloc/alloc.go`:
```go
package netalloc

import (
	"encoding/binary"
	"errors"
	"net"
)

var ErrNoFreeIPs = errors.New("no free IPs in subnet")

// Allocate возвращает первый свободный адрес подсети, пропуская адрес сети (.0),
// первый хост (.1, резерв VPN-сервера) и broadcast. Только IPv4.
func Allocate(cidr *net.IPNet, used []net.IP) (net.IP, error) {
	ip4 := cidr.IP.To4()
	if ip4 == nil {
		return nil, errors.New("only ipv4 subnets are supported")
	}
	n := binary.BigEndian.Uint32(ip4)
	mask := binary.BigEndian.Uint32(cidr.Mask.To4())
	broadcast := n | ^mask
	usedSet := map[uint32]bool{}
	for _, u := range used {
		if v := u.To4(); v != nil {
			usedSet[binary.BigEndian.Uint32(v)] = true
		}
	}
	for i := n + 2; i < broadcast; i++ {
		if !usedSet[i] {
			out := make(net.IP, 4)
			binary.BigEndian.PutUint32(out, i)
			return out, nil
		}
	}
	return nil, ErrNoFreeIPs
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/netalloc/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: client ip allocation"
```

---

### Task 10: Application service — пользовательские сценарии

**Files:**
- Create: `internal/service/service.go`
- Test: `internal/service/service_test.go`

**Interfaces:**
- Consumes: `store.Store`, `vpn.Provider`, `vpn.GenerateKeyPair`, `vpn.BuildClientConfig`, `patcher.Patch`, `netalloc.Allocate`, `config.Config`.
- Produces:
  - `service.New(cfg config.Config, st store.Store, vp vpn.Provider, ips IPListSource, log *slog.Logger) *Service`; `service.IPListSource interface { AllowedIPs() []string }` (реализация — `*routes.Service`).
  - Методы: `IsAdmin(id int64) bool`, `CheckAccess(ctx, telegramID) error`, `CreateConfig(ctx, telegramID, deviceName string) (CreatedConfig, error)`, `ListDevices(ctx, telegramID) ([]store.Peer, int, error)`, `DeleteConfig(ctx, telegramID, peerID int64) error`, `ServerForComplaint(ctx, telegramID) (serverID, displayName string, err error)`.
  - Тип `CreatedConfig{FileName, Content, DeviceName string}`; ошибки `ErrNoAccess`, `ErrLimitReached`, `ErrNotFound`, `ErrBadDeviceName`.
  Используют задачи 11, 14.

- [ ] **Step 1: Тесты**

`internal/service/service_test.go`:
```go
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
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/service/`
Expected: FAIL — пакет пуст

- [ ] **Step 3: Реализация**

`internal/service/service.go`:
```go
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/netalloc"
	"amnezia-manager-bot/internal/patcher"
	"amnezia-manager-bot/internal/store"
	"amnezia-manager-bot/internal/vpn"
)

var (
	ErrNoAccess      = errors.New("no access")
	ErrLimitReached  = errors.New("config limit reached")
	ErrNotFound      = errors.New("not found")
	ErrBadDeviceName = errors.New("invalid device name")
	ErrBadLimit      = errors.New("invalid limit")
)

// IPListSource — источник списка AllowedIPs (реализация — routes.Service).
type IPListSource interface {
	AllowedIPs() []string
}

var deviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,30}[a-zA-Z0-9]$`)

type CreatedConfig struct {
	FileName   string
	Content    string
	DeviceName string
}

type Service struct {
	cfg    config.Config
	store  store.Store
	vpn    vpn.Provider
	ips    IPListSource
	log    *slog.Logger
	admins map[int64]struct{}
}

func New(cfg config.Config, st store.Store, vp vpn.Provider, ips IPListSource, log *slog.Logger) *Service {
	admins := make(map[int64]struct{}, len(cfg.AdminIDs))
	for _, id := range cfg.AdminIDs {
		admins[id] = struct{}{}
	}
	return &Service{cfg: cfg, store: st, vpn: vp, ips: ips, log: log, admins: admins}
}

func (s *Service) IsAdmin(id int64) bool {
	_, ok := s.admins[id]
	return ok
}

func (s *Service) user(ctx context.Context, telegramID int64) (store.User, error) {
	u, err := s.store.GetUser(ctx, telegramID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return u, ErrNoAccess
	case err != nil:
		return u, err
	case !u.Enabled:
		return u, ErrNoAccess
	}
	return u, nil
}

func (s *Service) CheckAccess(ctx context.Context, telegramID int64) error {
	_, err := s.user(ctx, telegramID)
	return err
}

// CreateConfig: доступ и лимит → peer на сервере → патч AllowedIPs → метаданные в БД.
// Полный конфиг и приватный ключ нигде не сохраняются.
func (s *Service) CreateConfig(ctx context.Context, telegramID int64, deviceName string) (CreatedConfig, error) {
	if !deviceNameRe.MatchString(deviceName) {
		return CreatedConfig{}, ErrBadDeviceName
	}
	u, err := s.user(ctx, telegramID)
	if err != nil {
		return CreatedConfig{}, err
	}
	srv, err := s.cfg.DefaultServer()
	if err != nil {
		return CreatedConfig{}, err
	}
	ok, err := s.store.HasAccess(ctx, telegramID, srv.ID)
	if err != nil {
		return CreatedConfig{}, err
	}
	if !ok {
		return CreatedConfig{}, ErrNoAccess
	}
	active, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return CreatedConfig{}, err
	}
	if len(active) >= u.ConfigLimit {
		return CreatedConfig{}, ErrLimitReached
	}
	onServer, err := s.store.ListActivePeersOnServer(ctx, srv.ID)
	if err != nil {
		return CreatedConfig{}, err
	}
	_, cidr, err := net.ParseCIDR(srv.ClientCIDR)
	if err != nil {
		return CreatedConfig{}, fmt.Errorf("bad client_cidr: %w", err)
	}
	used := make([]net.IP, 0, len(onServer))
	for _, p := range onServer {
		used = append(used, net.ParseIP(p.ClientIP))
	}
	ip, err := netalloc.Allocate(cidr, used)
	if err != nil {
		return CreatedConfig{}, err
	}
	priv, pub, err := vpn.GenerateKeyPair()
	if err != nil {
		return CreatedConfig{}, err
	}
	if err := s.vpn.CreatePeer(ctx, srv.ID, pub, ip.String()); err != nil {
		return CreatedConfig{}, fmt.Errorf("create peer on server: %w", err)
	}
	patched, err := patcher.Patch(vpn.BuildClientConfig(srv, priv, ip.String()), s.ips.AllowedIPs())
	if err != nil {
		s.bestEffortRemove(ctx, srv.ID, pub)
		return CreatedConfig{}, err
	}
	p, err := s.store.CreatePeer(ctx, store.Peer{
		TelegramID: telegramID, ServerID: srv.ID, PeerID: pub,
		DeviceName: deviceName, ClientIP: ip.String(),
	})
	if err != nil {
		s.bestEffortRemove(ctx, srv.ID, pub)
		return CreatedConfig{}, fmt.Errorf("save peer: %w", err)
	}
	s.log.Info("config created", "user", telegramID, "peer_db_id", p.ID, "server", srv.ID, "device", deviceName)
	return CreatedConfig{FileName: deviceName + ".conf", Content: patched, DeviceName: deviceName}, nil
}

func (s *Service) bestEffortRemove(ctx context.Context, serverID, pub string) {
	if err := s.vpn.RemovePeer(ctx, serverID, pub); err != nil {
		s.log.Error("cleanup failed, peer left on server", "server", serverID)
	}
}

func (s *Service) ListDevices(ctx context.Context, telegramID int64) ([]store.Peer, int, error) {
	u, err := s.user(ctx, telegramID)
	if err != nil {
		return nil, 0, err
	}
	peers, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return nil, 0, err
	}
	return peers, u.ConfigLimit, nil
}

func (s *Service) DeleteConfig(ctx context.Context, telegramID int64, peerDBID int64) error {
	p, err := s.store.GetActivePeer(ctx, telegramID, peerDBID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.vpn.RemovePeer(ctx, p.ServerID, p.PeerID); err != nil {
		return fmt.Errorf("remove peer from server: %w", err)
	}
	if err := s.store.RevokePeer(ctx, p.ID); err != nil {
		return fmt.Errorf("revoke peer: %w", err)
	}
	s.log.Info("config deleted", "user", telegramID, "peer_db_id", p.ID, "server", p.ServerID)
	return nil
}

// ServerForComplaint возвращает сервер для контекста обращения:
// сервер первого конфига пользователя либо первый включённый.
func (s *Service) ServerForComplaint(ctx context.Context, telegramID int64) (string, string, error) {
	peers, err := s.store.ListActivePeers(ctx, telegramID)
	if err != nil {
		return "", "", err
	}
	id := ""
	if len(peers) > 0 {
		id = peers[0].ServerID
	} else if ss := s.cfg.EnabledServers(); len(ss) > 0 {
		id = ss[0].ID
	}
	srv, ok := s.cfg.ServerByID(id)
	if !ok {
		return "", "", errors.New("no server for complaint")
	}
	return srv.ID, srv.DisplayName, nil
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/service/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: core user flows in application service"
```

---
### Task 11: Application service — админ-операции

**Files:**
- Modify: `internal/service/service.go` (добавить методы в конец)
- Test: `internal/service/admin_test.go`

**Interfaces:**
- Consumes: `store.Store`, `config.Config`, хелпер `newSvc` из Task 10.
- Produces: `(*Service).AdminAddUser(ctx, telegramID int64, username string) (store.User, error)`, `AdminDisableUser(ctx, telegramID) error`, `AdminSetLimit(ctx, telegramID, limit int) error`, `AdminListUsers(ctx) ([]UserInfo, error)`; тип `UserInfo{ store.User; ActiveConfigs int }`; ошибка `ErrBadLimit` (уже объявлена в Task 10). Использует задача 14.

- [ ] **Step 1: Тесты**

`internal/service/admin_test.go`:
```go
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
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/service/ -run Admin`
Expected: FAIL — методы не определены

- [ ] **Step 3: Реализация (добавить в конец service.go)**

```go
// AdminAddUser регистрирует пользователя с лимитом по умолчанию
// и доступом ко всем включённым серверам.
func (s *Service) AdminAddUser(ctx context.Context, telegramID int64, username string) (store.User, error) {
	u := store.User{TelegramID: telegramID, Username: username, Enabled: true, ConfigLimit: s.cfg.DefaultLimit}
	if err := s.store.UpsertUser(ctx, u); err != nil {
		return store.User{}, err
	}
	for _, srv := range s.cfg.EnabledServers() {
		if err := s.store.GrantAccess(ctx, telegramID, srv.ID); err != nil {
			return store.User{}, err
		}
	}
	s.log.Info("user added", "user", telegramID)
	return u, nil
}

func (s *Service) AdminDisableUser(ctx context.Context, telegramID int64) error {
	if err := s.store.SetUserEnabled(ctx, telegramID, false); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.log.Info("user disabled", "user", telegramID)
	return nil
}

func (s *Service) AdminSetLimit(ctx context.Context, telegramID int64, limit int) error {
	if limit < 1 || limit > 50 {
		return ErrBadLimit
	}
	if err := s.store.SetUserLimit(ctx, telegramID, limit); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.log.Info("limit changed", "user", telegramID, "limit", limit)
	return nil
}

type UserInfo struct {
	store.User
	ActiveConfigs int
}

func (s *Service) AdminListUsers(ctx context.Context) ([]UserInfo, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	peers, err := s.store.ListActivePeersAll(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	for _, p := range peers {
		counts[p.TelegramID]++
	}
	out := make([]UserInfo, 0, len(users))
	for _, u := range users {
		out = append(out, UserInfo{User: u, ActiveConfigs: counts[u.TelegramID]})
	}
	return out, nil
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/service/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: admin operations in application service"
```

---

### Task 12: Alerts — статусные карточки серверов для админов

**Files:**
- Create: `internal/alerts/alerts.go`
- Test: `internal/alerts/alerts_test.go`

**Interfaces:**
- Consumes: `store.Store`, имена серверов `map[string]string`, список админов.
- Produces:
  - `alerts.Sender interface { SendMessage(chatID int64, text string) (messageID int64, err error); EditMessage(chatID, messageID int64, text string) error }`.
  - `alerts.NewManager(st store.Store, sender Sender, serverNames map[string]string, adminIDs []int64) *Manager`.
  - `(*Manager).ServerDown(ctx, serverID string)`, `(*Manager).ServerUp(ctx, serverID string)`, `(*Manager).Complaint(ctx, serverID string, c Complaint)`; тип `alerts.Complaint{AuthorID int64; Username, Text string; At time.Time}`.
  Используют задачи 13, 14, 15.

- [ ] **Step 1: Тесты**

`internal/alerts/alerts_test.go`:
```go
package alerts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"amnezia-manager-bot/internal/store"
	"amnezia-manager-bot/internal/store/memory"
)

type fakeSender struct {
	mu     sync.Mutex
	sent   []string
	edited map[string]string
	nextID int64
	failEd bool
}

func newFakeSender() *fakeSender { return &fakeSender{edited: map[string]string{}, nextID: 500} }

func (f *fakeSender) SendMessage(chatID int64, text string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.sent = append(f.sent, text)
	return f.nextID, nil
}

func (f *fakeSender) EditMessage(chatID, messageID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failEd {
		return fmt.Errorf("edit failed")
	}
	f.edited[fmt.Sprintf("%d:%d", chatID, messageID)] = text
	return nil
}

func newMgr(t *testing.T) (*Manager, *fakeSender, *memory.MemoryStore) {
	t.Helper()
	st := memory.New()
	fs := newFakeSender()
	m := NewManager(st, fs, map[string]string{"s1": "SPB-1"}, []int64{10, 20})
	return m, fs, st
}

func TestServerDownCreatesCardPerAdmin(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	if len(fs.sent) != 2 {
		t.Fatalf("sent = %d, want 2", len(fs.sent))
	}
	for _, admin := range []int64{10, 20} {
		sm, err := st.GetStatusMessage(ctx, "s1", admin)
		if err != nil {
			t.Fatalf("admin %d: %v", admin, err)
		}
		if sm.MessageID == 0 {
			t.Fatal("message id must be stored")
		}
	}
}

func TestRecoveryAndNewIncidentEditSameMessage(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	first, _ := st.GetStatusMessage(ctx, "s1", 10)
	m.ServerUp(ctx, "s1")
	m.ServerDown(ctx, "s1") // новый инцидент
	second, _ := st.GetStatusMessage(ctx, "s1", 10)
	if first.MessageID != second.MessageID {
		t.Fatalf("message id changed: %d -> %d", first.MessageID, second.MessageID)
	}
	if len(fs.sent) != 2 {
		t.Fatalf("no new messages expected after first, sent=%d", len(fs.sent))
	}
	key := fmt.Sprintf("10:%d", second.MessageID)
	if _, ok := fs.edited[key]; !ok {
		t.Fatalf("no edit recorded: %v", fs.edited)
	}
}

func TestComplaintUpdatesCard(t *testing.T) {
	m, fs, _ := newMgr(t)
	ctx := context.Background()
	m.Complaint(ctx, "s1", Complaint{AuthorID: 100, Username: "u100", Text: "не работает", At: time.Now()})
	if len(fs.sent) != 2 {
		t.Fatalf("complaint must create card if missing, sent=%d", len(fs.sent))
	}
	m.Complaint(ctx, "s1", Complaint{AuthorID: 101, Username: "u101", Text: "тоже не работает", At: time.Now()})
	if len(fs.sent) != 2 {
		t.Fatalf("second complaint must edit existing card, sent=%d", len(fs.sent))
	}
	found := false
	for _, txt := range fs.edited {
		if strings.Contains(txt, "u101") && strings.Contains(txt, "тоже не работает") {
			found = true
		}
	}
	if !found {
		t.Fatalf("complaint text not in card edits: %v", fs.edited)
	}
}

func TestCardContents(t *testing.T) {
	m, fs, _ := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	if !strings.Contains(fs.sent[0], "SPB-1") || !strings.Contains(fs.sent[0], "недоступен") {
		t.Fatalf("down card: %q", fs.sent[0])
	}
	m.ServerUp(ctx, "s1")
	found := false
	for _, txt := range fs.edited {
		if strings.Contains(txt, "работает") && strings.Contains(txt, "SPB-1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("up card not found in %v", fs.edited)
	}
}

func TestEditFailureSendsNewMessage(t *testing.T) {
	m, fs, st := newMgr(t)
	ctx := context.Background()
	m.ServerDown(ctx, "s1")
	fs.mu.Lock()
	fs.failEd = true
	fs.mu.Unlock()
	before := len(fs.sent)
	m.ServerUp(ctx, "s1") // edit упадёт → отправит новое сообщение и перепишет id
	if len(fs.sent) <= before {
		t.Fatal("expected new message on edit failure")
	}
	sm, err := st.GetStatusMessage(ctx, "s1", 10)
	if err != nil || sm.MessageID == 0 {
		t.Fatalf("stored: %v %+v", err, sm)
	}
}

var _ store.Store = (*memory.MemoryStore)(nil)
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/alerts/`
Expected: FAIL — пакет пуст

- [ ] **Step 3: Реализация**

`internal/alerts/alerts.go`:
```go
package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"amnezia-manager-bot/internal/store"
)

// Sender — минимальный интерфейс отправки Telegram-сообщений (реализация — tgbot.Sender).
type Sender interface {
	SendMessage(chatID int64, text string) (messageID int64, err error)
	EditMessage(chatID, messageID int64, text string) error
}

type Complaint struct {
	AuthorID int64
	Username string
	Text     string
	At       time.Time
}

// Manager ведёт по одному статусному сообщению на (сервер, админ) и редактирует его:
// недоступен / восстановлен / новое обращение. Новое сообщение — только если его ещё нет.
type Manager struct {
	store       store.Store
	sender      Sender
	serverNames map[string]string
	adminIDs    []int64
	log         *slog.Logger

	mu         sync.Mutex
	downSince  map[string]time.Time
	complaints map[string][]Complaint
}

func NewManager(st store.Store, sender Sender, serverNames map[string]string, adminIDs []int64) *Manager {
	return &Manager{
		store:       st,
		sender:      sender,
		serverNames: serverNames,
		adminIDs:    adminIDs,
		log:         slog.Default(),
		downSince:   map[string]time.Time{},
		complaints:  map[string][]Complaint{},
	}
}

func (m *Manager) name(serverID string) string {
	if n, ok := m.serverNames[serverID]; ok && n != "" {
		return n
	}
	return serverID
}

func (m *Manager) ServerDown(ctx context.Context, serverID string) {
	m.mu.Lock()
	if _, ok := m.downSince[serverID]; !ok {
		m.downSince[serverID] = time.Now()
	}
	m.mu.Unlock()
	m.updateCards(ctx, serverID)
}

func (m *Manager) ServerUp(ctx context.Context, serverID string) {
	m.mu.Lock()
	delete(m.downSince, serverID)
	m.mu.Unlock()
	m.updateCards(ctx, serverID)
}

func (m *Manager) Complaint(ctx context.Context, serverID string, c Complaint) {
	m.mu.Lock()
	m.complaints[serverID] = append(m.complaints[serverID], c)
	if len(m.complaints[serverID]) > 5 {
		m.complaints[serverID] = m.complaints[serverID][len(m.complaints[serverID])-5:]
	}
	m.mu.Unlock()
	m.updateCards(ctx, serverID)
}

func (m *Manager) card(serverID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := m.name(serverID)
	var text string
	if since, ok := m.downSince[serverID]; ok {
		text = fmt.Sprintf("🔴 Сервер «%s» недоступен с %s", name, since.Format("02.01 15:04"))
	} else {
		text = fmt.Sprintf("🟢 Сервер «%s» работает", name)
	}
	if cs := m.complaints[serverID]; len(cs) > 0 {
		text += "\n\nОбращения:"
		for i := len(cs) - 1; i >= 0 && i >= len(cs)-3; i-- {
			c := cs[i]
			who := c.Username
			if who == "" {
				who = fmt.Sprintf("id%d", c.AuthorID)
			}
			body := c.Text
			if len(body) > 300 {
				body = body[:300] + "…"
			}
			text += fmt.Sprintf("\n• %s %s [%d]: %s", c.At.Format("02.01 15:04"), who, c.AuthorID, body)
		}
	}
	return text
}

func (m *Manager) updateCards(ctx context.Context, serverID string) {
	text := m.card(serverID)
	for _, admin := range m.adminIDs {
		sm, err := m.store.GetStatusMessage(ctx, serverID, admin)
		if err == nil {
			if m.sender.EditMessage(sm.ChatID, sm.MessageID, text) == nil {
				continue
			}
			m.log.Warn("edit status message failed, sending new", "server", serverID, "admin", admin)
		}
		msgID, err := m.sender.SendMessage(admin, text)
		if err != nil {
			m.log.Error("send status message failed", "server", serverID, "admin", admin, "err", err)
			continue
		}
		if err := m.store.SaveStatusMessage(ctx, store.StatusMessage{
			ServerID: serverID, AdminID: admin, ChatID: admin, MessageID: msgID,
		}); err != nil {
			m.log.Error("save status message failed", "server", serverID, "admin", admin, "err", err)
		}
	}
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/alerts/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: per-server status cards for admin alerts"
```

---
### Task 13: Monitor — проверка доступности серверов

**Files:**
- Create: `internal/monitor/monitor.go`
- Test: `internal/monitor/monitor_test.go`

**Interfaces:**
- Consumes: `vpn.Provider.HealthCheck`, `config.ServerConfig`.
- Produces: `monitor.New(vp vpn.Provider, a Alerts, servers []config.ServerConfig, interval, threshold time.Duration, log *slog.Logger) *Monitor`; `monitor.Alerts interface { ServerDown(ctx context.Context, serverID string); ServerUp(ctx context.Context, serverID string) }` (реализация — `*alerts.Manager`); `(*Monitor).Run(ctx)`, `(*Monitor).CheckNow(ctx)`. Использует задача 15.

- [ ] **Step 1: Тесты**

`internal/monitor/monitor_test.go`:
```go
package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"amnezia-manager-bot/internal/config"
)

var errDown = errors.New("ssh unreachable")

type fakeVPN struct {
	mu   sync.Mutex
	errs map[string]error
}

func (f *fakeVPN) setErr(id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[id] = err
}

func (f *fakeVPN) HealthCheck(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[id]
}

func (f *fakeVPN) CreatePeer(context.Context, string, string, string) error { return nil }
func (f *fakeVPN) RemovePeer(context.Context, string, string) error         { return nil }

type fakeAlerts struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAlerts) ServerDown(_ context.Context, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "down:"+id)
}

func (f *fakeAlerts) ServerUp(_ context.Context, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "up:"+id)
}

func (f *fakeAlerts) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func newMon(t *testing.T) (*Monitor, *fakeVPN, *fakeAlerts) {
	t.Helper()
	fv := &fakeVPN{errs: map[string]error{}}
	fa := &fakeAlerts{}
	m := New(fv, fa, []config.ServerConfig{{ID: "s1"}, {ID: "s2"}}, time.Minute, 2*time.Minute, nil)
	return m, fv, fa
}

func TestThreshold(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }

	fv.setErr("s1", errDown)
	m.CheckNow(ctx) // t0: сбой начался
	if calls := fa.snapshot(); len(calls) != 0 {
		t.Fatalf("early alert: %v", calls)
	}
	now = now.Add(2 * time.Minute)
	m.CheckNow(ctx) // порог достигнут
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
	now = now.Add(time.Minute)
	m.CheckNow(ctx) // всё ещё недоступен — не дублируем
	if calls := fa.snapshot(); len(calls) != 1 {
		t.Fatalf("duplicated: %v", calls)
	}
	fv.setErr("s1", nil)
	m.CheckNow(ctx) // восстановился
	if calls := fa.snapshot(); len(calls) != 2 || calls[1] != "up:s1" {
		t.Fatalf("calls = %v", calls)
	}
	m.CheckNow(ctx)
	if calls := fa.snapshot(); len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestFlapUnderThresholdResets(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }

	fv.setErr("s1", errDown)
	m.CheckNow(ctx)
	now = now.Add(time.Minute) // < порога 2м
	fv.setErr("s1", nil)
	m.CheckNow(ctx) // флап исправился до порога — алертов нет
	if calls := fa.snapshot(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
	fv.setErr("s1", errDown)
	m.CheckNow(ctx) // downSince сброшен, отсчёт заново
	now = now.Add(2 * time.Minute)
	m.CheckNow(ctx)
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestServersIndependent(t *testing.T) {
	m, fv, fa := newMon(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }
	fv.setErr("s1", errDown)
	fv.setErr("s2", errDown)
	m.CheckNow(ctx)
	now = now.Add(2 * time.Minute)
	fv.setErr("s2", nil)
	m.CheckNow(ctx) // s1 алерт, s2 восстановился до алерта
	if calls := fa.snapshot(); len(calls) != 1 || calls[0] != "down:s1" {
		t.Fatalf("calls = %v", calls)
	}
}
```

- [ ] **Step 2: Упасть**

Run: `go test ./internal/monitor/`
Expected: FAIL — пакет пуст

- [ ] **Step 3: Реализация**

`internal/monitor/monitor.go`:
```go
package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/vpn"
)

// Alerts — подмножество alerts.Manager, нужное монитору.
type Alerts interface {
	ServerDown(ctx context.Context, serverID string)
	ServerUp(ctx context.Context, serverID string)
}

type srvState struct {
	downSince time.Time
	alerted   bool
}

// Monitor периодически проверяет серверы по SSH и уведомляет о недоступности
// дольше порога; восстановление тоже уведомляет (однократно на инцидент).
type Monitor struct {
	vpn       vpn.Provider
	alerts    Alerts
	servers   []config.ServerConfig
	interval  time.Duration
	threshold time.Duration
	log       *slog.Logger

	mu    sync.Mutex
	state map[string]*srvState
	now   func() time.Time
}

func New(vp vpn.Provider, a Alerts, servers []config.ServerConfig, interval, threshold time.Duration, log *slog.Logger) *Monitor {
	if log == nil {
		log = slog.Default()
	}
	states := map[string]*srvState{}
	for _, s := range servers {
		states[s.ID] = &srvState{}
	}
	return &Monitor{
		vpn: vp, alerts: a, servers: servers,
		interval: interval, threshold: threshold, log: log,
		state: states, now: time.Now,
	}
}

func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		m.CheckNow(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CheckNow проверяет все серверы один раз.
func (m *Monitor) CheckNow(ctx context.Context) {
	for _, s := range m.servers {
		m.checkServer(ctx, s.ID)
	}
}

func (m *Monitor) checkServer(ctx context.Context, serverID string) {
	err := m.vpn.HealthCheck(ctx, serverID)
	m.mu.Lock()
	st := m.state[serverID]
	if st == nil {
		st = &srvState{}
		m.state[serverID] = st
	}
	var down, up bool
	if err != nil {
		if st.downSince.IsZero() {
			st.downSince = m.now()
		}
		if !st.alerted && m.now().Sub(st.downSince) >= m.threshold {
			st.alerted = true
			down = true
		}
	} else {
		was := st.alerted
		*st = srvState{}
		if was {
			up = true
		}
	}
	m.mu.Unlock()
	if down {
		m.alerts.ServerDown(ctx, serverID)
		m.log.Warn("server down alert", "server", serverID)
	}
	if up {
		m.alerts.ServerUp(ctx, serverID)
		m.log.Info("server recovered", "server", serverID)
	}
}
```

- [ ] **Step 4: Пройти + коммит**

Run: `go test ./internal/monitor/ -v`
Expected: PASS

```bash
git add -A && git commit -m "feat: server health monitor with down threshold"
```

---

### Task 14: Telegram-бот

**Files:**
- Create: `internal/tgbot/bot.go`, `internal/tgbot/handlers_user.go`, `internal/tgbot/handlers_admin.go`, `internal/tgbot/state.go`, `internal/tgbot/texts.go`
- Test: `internal/tgbot/state_test.go`

**Interfaces:**
- Consumes: `service.Service` (все методы задач 10–11), `alerts.Manager.Complaint`.
- Produces: `tgbot.New(api *tgbotapi.BotAPI, svc *service.Service, a *alerts.Manager, log *slog.Logger, serverNames map[string]string) *Bot`; `(*Bot).Run(ctx) error`; `tgbot.NewSender(api *tgbotapi.BotAPI) Sender` — реализует `alerts.Sender`. Использует задача 15.

- [ ] **Step 1: Тест состояния диалога**

`internal/tgbot/state_test.go`:
```go
package tgbot

import (
	"testing"
	"time"
)

func TestStates(t *testing.T) {
	s := newStates()
	now := time.Now()
	s.now = func() time.Time { return now }

	if s.get(1) != stateNone {
		t.Fatal("default must be none")
	}
	s.set(1, stateDeviceName)
	if s.get(1) != stateDeviceName {
		t.Fatal("state not stored")
	}
	now = now.Add(stateTTL + time.Second)
	if s.get(1) != stateNone {
		t.Fatal("expired state must be none")
	}
	s.set(1, stateComplaint)
	s.clear(1)
	if s.get(1) != stateNone {
		t.Fatal("cleared state must be none")
	}
}
```

- [ ] **Step 2: Упасть**

```bash
go get github.com/go-telegram-bot-api/telegram-bot-api/v5
mkdir -p internal/tgbot
go test ./internal/tgbot/
```
Expected: FAIL — newStates undefined

- [ ] **Step 3: Реализация state.go**

`internal/tgbot/state.go`:
```go
package tgbot

import (
	"sync"
	"time"
)

type stateKind int

const (
	stateNone stateKind = iota
	stateDeviceName
	stateComplaint
)

const stateTTL = 10 * time.Minute

type userState struct {
	kind    stateKind
	expires time.Time
}

type states struct {
	mu  sync.Mutex
	m   map[int64]*userState
	now func() time.Time
}

func newStates() *states {
	return &states{m: map[int64]*userState{}, now: time.Now}
}

func (s *states) set(userID int64, k stateKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = &userState{kind: k, expires: s.now().Add(stateTTL)}
}

func (s *states) get(userID int64) stateKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[userID]
	if !ok || s.now().After(st.expires) {
		delete(s.m, userID)
		return stateNone
	}
	return st.kind
}

func (s *states) clear(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, userID)
}
```

Run: `go test ./internal/tgbot/ -v` — PASS. Далее хендлеры (без unit-тестов, проверяются сборкой и ручным smoke в Task 15).

- [ ] **Step 4: texts.go и bot.go**

`internal/tgbot/texts.go`:
```go
package tgbot

import (
	"errors"

	"amnezia-manager-bot/internal/service"
)

const (
	textNoAccess        = "Нет доступа."
	textMenu            = "Главное меню:"
	textAskDeviceName   = "Введите имя устройства (3–32 символа: латинские буквы, цифры, «-», «_»)."
	textAskComplaint    = "Опишите проблему одним сообщением."
	textConfigOnce      = "Файл выдаётся один раз и повторно не высылается. Сохраните его. Потерянный конфиг нужно удалить и создать заново."
	textComplaintSent   = "Обращение отправлено администраторам."
	textInstruction     = "Как подключиться:\n1. Установите приложение AmneziaWG (amnezia.org; iOS — App Store, Android — Google Play).\n2. Откройте полученный .conf файл в приложении (Add tunnel → Import file(s)).\n3. Включите туннель.\n\nЕсли конфиг потерян — удалите устройство в боте и создайте заново."
	textDeleted         = "Конфиг удалён."
	textUnknownCommand  = "Неизвестная команда."
	textServiceDown     = "Сервис временно недоступен, попробуйте позже."
)

// userMessage переводит ошибки сервиса в тексты без внутренних деталей.
func userMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrNoAccess):
		return textNoAccess
	case errors.Is(err, service.ErrLimitReached):
		return "Достигнут лимит активных конфигов. Удалите ненужный или обратитесь к администратору."
	case errors.Is(err, service.ErrBadDeviceName):
		return "Некорректное имя устройства: 3–32 символа, латинские буквы, цифры, «-», «_»."
	case errors.Is(err, service.ErrNotFound):
		return "Конфиг не найден."
	case errors.Is(err, service.ErrBadLimit):
		return "Лимит должен быть числом от 1 до 50."
	default:
		return textServiceDown
	}
}
```

`internal/tgbot/bot.go`:
```go
package tgbot

import (
	"context"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/service"
)

type Bot struct {
	api         *tgbotapi.BotAPI
	svc         *service.Service
	alerts      *alerts.Manager
	st          *states
	log         *slog.Logger
	serverNames map[string]string
}

func New(api *tgbotapi.BotAPI, svc *service.Service, a *alerts.Manager, log *slog.Logger, serverNames map[string]string) *Bot {
	return &Bot{api: api, svc: svc, alerts: a, st: newStates(), log: log, serverNames: serverNames}
}

// Sender адаптирует telegram-bot-api к alerts.Sender.
type Sender struct{ api *tgbotapi.BotAPI }

func NewSender(api *tgbotapi.BotAPI) Sender { return Sender{api: api} }

func (s Sender) SendMessage(chatID int64, text string) (int64, error) {
	m, err := s.api.Send(tgbotapi.NewMessage(chatID, text))
	if err != nil {
		return 0, err
	}
	return int64(m.MessageID), nil
}

func (s Sender) EditMessage(chatID, messageID int64, text string) error {
	edit := tgbotapi.NewEditMessageText(chatID, int(messageID), text)
	_, err := s.api.Send(edit)
	return err
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)
	b.log.Info("bot started", "username", b.api.Self.UserName)
	for {
		select {
		case <-ctx.Done():
			return nil
		case upd := <-updates:
			b.handleUpdate(ctx, upd)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u tgbotapi.Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *tgbotapi.Message) {
	if m.From == nil {
		return
	}
	uid := m.From.ID
	if m.IsCommand() {
		b.handleCommand(ctx, uid, m)
		return
	}
	switch b.st.get(uid) {
	case stateDeviceName:
		b.st.clear(uid)
		b.handleDeviceName(ctx, uid, int64(m.Chat.ID), m.Text)
	case stateComplaint:
		b.st.clear(uid)
		b.handleComplaintText(ctx, uid, m.From.UserName, int64(m.Chat.ID), m.Text)
	}
}

func (b *Bot) handleCommand(ctx context.Context, uid int64, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	switch m.Command() {
	case "start":
		b.handleStart(ctx, uid, chatID)
	case "adduser":
		if b.adminOnly(uid, chatID) {
			b.cmdAddUser(ctx, m)
		}
	case "disableuser":
		if b.adminOnly(uid, chatID) {
			b.cmdDisableUser(ctx, m)
		}
	case "setlimit":
		if b.adminOnly(uid, chatID) {
			b.cmdSetLimit(ctx, m)
		}
	case "users":
		if b.adminOnly(uid, chatID) {
			b.cmdUsers(ctx, m)
		}
	default:
		b.sendText(chatID, textUnknownCommand)
	}
}

func (b *Bot) adminOnly(uid int64, chatID int64) bool {
	if !b.svc.IsAdmin(uid) {
		b.sendText(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) handleCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	b.answerCallback(q.ID)
	if q.Message == nil {
		return
	}
	chatID := int64(q.Message.Chat.ID)
	uid := q.From.ID
	if !b.userAllowed(ctx, uid, chatID) {
		return
	}
	switch {
	case q.Data == "create":
		b.st.set(uid, stateDeviceName)
		b.sendText(chatID, textAskDeviceName)
	case q.Data == "devices":
		b.showDevices(ctx, uid, chatID)
	case q.Data == "help":
		b.sendText(chatID, textInstruction)
	case q.Data == "complaint":
		b.st.set(uid, stateComplaint)
		b.sendText(chatID, textAskComplaint)
	case len(q.Data) > 4 && q.Data[:4] == "del:":
		b.confirmDelete(chatID, q.Data[4:])
	case len(q.Data) > 6 && q.Data[:6] == "delok:":
		b.doDelete(ctx, uid, chatID, q.Data[6:])
	}
}

func (b *Bot) userAllowed(ctx context.Context, uid int64, chatID int64) bool {
	if b.svc.IsAdmin(uid) {
		return true
	}
	if err := b.svc.CheckAccess(ctx, uid); err != nil {
		b.sendText(chatID, textNoAccess)
		return false
	}
	return true
}

func (b *Bot) handleStart(ctx context.Context, uid int64, chatID int64) {
	if !b.svc.IsAdmin(uid) {
		if err := b.svc.CheckAccess(ctx, uid); err != nil {
			b.sendText(chatID, textNoAccess)
			return
		}
	}
	msg := tgbotapi.NewMessage(chatID, textMenu)
	msg.ReplyMarkup = menuKeyboard()
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send menu failed", "err", err)
	}
}

func menuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Создать конфиг", "create"),
			tgbotapi.NewInlineKeyboardButtonData("Мои устройства", "devices"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Инструкция", "help"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Пожаловаться", "complaint"),
		),
	)
}

func (b *Bot) sendText(chatID int64, text string) {
	if _, err := b.api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		b.log.Error("send message failed", "chat", chatID, "err", err)
	}
}

func (b *Bot) answerCallback(id string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(id, "")); err != nil {
		b.log.Error("answer callback failed", "err", err)
	}
}
```

- [ ] **Step 5: handlers_user.go и handlers_admin.go**

`internal/tgbot/handlers_user.go`:
```go
package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/service"
)

func (b *Bot) handleDeviceName(ctx context.Context, uid int64, chatID int64, name string) {
	cc, err := b.svc.CreateConfig(ctx, uid, strings.TrimSpace(name))
	if err != nil {
		b.log.Error("create config failed", "user", uid, "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: cc.FileName, Bytes: []byte(cc.Content)})
	if _, err := b.api.Send(doc); err != nil {
		b.log.Error("send document failed", "user", uid, "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	b.sendText(chatID, textConfigOnce)
}

func (b *Bot) showDevices(ctx context.Context, uid int64, chatID int64) {
	peers, limit, err := b.svc.ListDevices(ctx, uid)
	if err != nil {
		b.sendText(chatID, userMessage(err))
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Активных конфигов: %d из %d.\n", len(peers), limit)
	if len(peers) == 0 {
		sb.WriteString("Конфигов нет.")
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range peers {
		fmt.Fprintf(&sb, "• %s (создан %s)\n", p.DeviceName, p.CreatedAt.Format("02.01.2006"))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Удалить: "+p.DeviceName, fmt.Sprintf("del:%d", p.ID)),
		))
	}
	msg := tgbotapi.NewMessage(chatID, sb.String())
	if len(rows) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send devices failed", "err", err)
	}
}

func (b *Bot) confirmDelete(chatID int64, idStr string) {
	msg := tgbotapi.NewMessage(chatID, "Удалить конфиг? Действие необратимо.")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Да, удалить", "delok:"+idStr),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "devices"),
		),
	)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send confirm failed", "err", err)
	}
}

func (b *Bot) doDelete(ctx context.Context, uid int64, chatID int64, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.sendText(chatID, userMessage(service.ErrNotFound))
		return
	}
	if err := b.svc.DeleteConfig(ctx, uid, id); err != nil {
		b.log.Error("delete config failed", "user", uid, "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, textDeleted)
}

func (b *Bot) handleComplaintText(ctx context.Context, uid int64, username string, chatID int64, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		b.sendText(chatID, textAskComplaint)
		return
	}
	serverID, _, err := b.svc.ServerForComplaint(ctx, uid)
	if err != nil {
		b.log.Error("complaint server failed", "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	b.alerts.Complaint(ctx, serverID, alerts.Complaint{AuthorID: uid, Username: username, Text: text})
	b.log.Info("complaint registered", "user", uid, "server", serverID)
	b.sendText(chatID, textComplaintSent)
}
```

`internal/tgbot/handlers_admin.go`:
```go
package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) cmdAddUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) == 0 {
		b.sendText(chatID, "Использование: /adduser <telegram_id> [username]")
		return
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		b.sendText(chatID, "Некорректный Telegram ID.")
		return
	}
	username := ""
	if len(args) > 1 {
		username = args[1]
	}
	u, err := b.svc.AdminAddUser(ctx, id, username)
	if err != nil {
		b.log.Error("adduser failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Пользователь %d добавлен, лимит %d.", u.TelegramID, u.ConfigLimit))
}

func (b *Bot) cmdDisableUser(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	id, err := strconv.ParseInt(strings.TrimSpace(m.CommandArguments()), 10, 64)
	if err != nil || id <= 0 {
		b.sendText(chatID, "Использование: /disableuser <telegram_id>")
		return
	}
	if err := b.svc.AdminDisableUser(ctx, id); err != nil {
		b.log.Error("disableuser failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Пользователь %d отключён.", id))
}

func (b *Bot) cmdSetLimit(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	args := strings.Fields(m.CommandArguments())
	if len(args) != 2 {
		b.sendText(chatID, "Использование: /setlimit <telegram_id> <limit>")
		return
	}
	id, err1 := strconv.ParseInt(args[0], 10, 64)
	limit, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil || id <= 0 {
		b.sendText(chatID, "Некорректные аргументы.")
		return
	}
	if err := b.svc.AdminSetLimit(ctx, id, limit); err != nil {
		b.log.Error("setlimit failed", "err", err)
		b.sendText(chatID, userMessage(err))
		return
	}
	b.sendText(chatID, fmt.Sprintf("Лимит пользователя %d изменён на %d.", id, limit))
}

func (b *Bot) cmdUsers(ctx context.Context, m *tgbotapi.Message) {
	chatID := int64(m.Chat.ID)
	users, err := b.svc.AdminListUsers(ctx)
	if err != nil {
		b.log.Error("users failed", "err", err)
		b.sendText(chatID, textServiceDown)
		return
	}
	var sb strings.Builder
	sb.WriteString("Пользователи:\n")
	for _, u := range users {
		status := "✅"
		if !u.Enabled {
			status = "⛔️"
		}
		fmt.Fprintf(&sb, "%s %d @%s — активных %d, лимит %d\n", status, u.TelegramID, u.Username, u.ActiveConfigs, u.ConfigLimit)
	}
	b.sendText(chatID, sb.String())
}
```

- [ ] **Step 6: Сборка, тесты, коммит**

Run: `go build ./... && go vet ./... && go test ./internal/tgbot/ -v`
Expected: OK, PASS

```bash
git add -A && git commit -m "feat: telegram bot handlers"
```

---
### Task 15: main — wiring, healthz, graceful shutdown

**Files:**
- Create: `cmd/bot/main.go`

**Interfaces:**
- Consumes: все пакеты задач 1–14.
- Produces: рабочий бинарник `bin/amnezia-bot`.

- [ ] **Step 1: Реализация**

`cmd/bot/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/db"
	"amnezia-manager-bot/internal/monitor"
	"amnezia-manager-bot/internal/routes"
	"amnezia-manager-bot/internal/service"
	"amnezia-manager-bot/internal/store/postgres"
	tgbot "amnezia-manager-bot/internal/tgbot"
	"amnezia-manager-bot/internal/vpn/sshprovider"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	st := postgres.New(pool)
	vpnProv := sshprovider.New(cfg, log)

	routesSvc, err := routes.New(cfg.Routes.URL, log)
	if err != nil {
		return err
	}
	go routesSvc.Run(ctx, cfg.Routes.RefreshInterval)

	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return fmt.Errorf("telegram api: %w", err)
	}

	names := map[string]string{}
	for _, s := range cfg.Servers {
		names[s.ID] = s.DisplayName
	}

	alertsMgr := alerts.NewManager(st, tgbot.NewSender(api), names, cfg.AdminIDs)
	svc := service.New(cfg, st, vpnProv, routesSvc, log)
	mon := monitor.New(vpnProv, alertsMgr, cfg.EnabledServers(), cfg.Monitor.CheckInterval, cfg.Monitor.DownThreshold, log)
	go mon.Run(ctx)

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(pingCtx); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if err := srv.ListenAndServe(); err != nil {
			log.Error("health server stopped", "err", err)
		}
	}()

	b := tgbot.New(api, svc, alertsMgr, log, names)
	return b.Run(ctx)
}
```

- [ ] **Step 2: Сборка + smoke ошибок конфигурации**

```bash
go mod tidy && go build ./... && go vet ./...
make build
./bin/amnezia-bot -config /nonexistent.yaml            # exit 1, "load config: read config ..."
BOT_TOKEN=x DATABASE_URL=x SSH_PRIVATE_KEY=x ./bin/amnezia-bot -config configs/config.example.yaml   # exit 1, ошибка подключения к БД (краткая, без секретов)
```
Expected: понятные ошибки, никаких паник.

- [ ] **Step 3: Полный тестовый прогон + коммит**

Run: `make test`
Expected: PASS

```bash
git add -A && git commit -m "feat: wire everything in main with healthz endpoint"
```

---

### Task 16: Deploy — Docker, Kubernetes, серверные скрипты

**Files:**
- Create: `deploy/docker/Dockerfile`
- Create: `deploy/k8s/deployment.yaml`, `deploy/k8s/configmap.yaml`, `deploy/k8s/secret.example.yaml`
- Create: `deploy/server/awg-peer-add`, `deploy/server/awg-peer-remove`, `deploy/server/awg-health`, `deploy/server/test_scripts.sh`, `deploy/server/amnezia-bot.sudoers`, `deploy/server/README.md`

**Interfaces:**
- Consumes: бинарник из Task 15, sudo-интерфейс из Task 8.
- Produces: артефакты развёртывания; скрипты с точно теми именами команд, которые генерирует sshprovider.

- [ ] **Step 1: Серверные скрипты**

`deploy/server/awg-peer-add`:
```sh
#!/bin/sh
# awg-peer-add IFACE PUBKEY IP — добавляет peer в конфиг AmneziaWG и применяет на лету.
set -eu
[ "$#" -eq 3 ] || { echo "usage: awg-peer-add IFACE PUBKEY IP" >&2; exit 64; }
iface="$1"; pub="$2"; ip="$3"
conf="${AWG_CONF_DIR:-/etc/amnezia/amnezia-wg}/$iface.conf"
case "$pub" in *[!A-Za-z0-9+/=]*) echo "bad pubkey" >&2; exit 64;; esac
echo "$ip" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || { echo "bad ip" >&2; exit 64; }
[ -f "$conf" ] || { echo "no config $conf" >&2; exit 66; }
if grep -Eq "^[[:space:]]*PublicKey[[:space:]]*=[[:space:]]*$pub[[:space:]]*$" "$conf"; then
	echo "PEER_EXISTS" >&2
	exit 3
fi
tmp="$(mktemp)"
{ cat "$conf"; printf '\n# managed-by amnezia-bot\n[Peer]\nPublicKey = %s\nAllowedIPs = %s/32\n' "$pub" "$ip"; } > "$tmp"
cat "$tmp" > "$conf"
rm -f "$tmp"
awg set "$iface" peer "$pub" allowed-ips "$ip/32"
```

`deploy/server/awg-peer-remove`:
```sh
#!/bin/sh
# awg-peer-remove IFACE PUBKEY — удаляет peer из конфига и из рантайма.
set -eu
[ "$#" -eq 2 ] || { echo "usage: awg-peer-remove IFACE PUBKEY" >&2; exit 64; }
iface="$1"; pub="$2"
conf="${AWG_CONF_DIR:-/etc/amnezia/amnezia-wg}/$iface.conf"
case "$pub" in *[!A-Za-z0-9+/=]*) echo "bad pubkey" >&2; exit 64;; esac
[ -f "$conf" ] || { echo "no config $conf" >&2; exit 66; }
if ! grep -Eq "^[[:space:]]*PublicKey[[:space:]]*=[[:space:]]*$pub[[:space:]]*$" "$conf"; then
	echo "PEER_NOT_FOUND" >&2
	exit 3
fi
tmp="$(mktemp)"
awk -v pub="$pub" '
	function flushPeer() {
		if (inpeer && keep) printf "%s", peerbuf
		inpeer = 0; keep = 1; peerbuf = ""
	}
	/^# managed-by amnezia-bot/ { flushPeer(); inpeer = 1; peerbuf = $0 "\n"; next }
	/^\[Peer\]/ { if (!inpeer) { peerbuf = "" }; inpeer = 1; peerbuf = peerbuf $0 "\n"; next }
	inpeer { peerbuf = peerbuf $0 "\n"; if ($0 ~ "PublicKey = " pub "$") keep = 0; next }
	{ flushPeer(); print }
	END { flushPeer() }
' "$conf" > "$tmp"
cat "$tmp" > "$conf"
rm -f "$tmp"
# peer мог уже отсутствовать в рантайме — это не ошибка
awg set "$iface" peer "$pub" remove || true
```

`deploy/server/awg-health`:
```sh
#!/bin/sh
# awg-health IFACE — проверка, что интерфейс AmneziaWG жив.
set -eu
[ "$#" -eq 1 ] || { echo "usage: awg-health IFACE" >&2; exit 64; }
iface="$1"
awg show "$iface" >/dev/null
echo ok
```

`deploy/server/test_scripts.sh` (запускается в alpine-контейнере, проверяет скрипты с заглушкой awg):
```sh
#!/bin/sh
set -eu
cd /s
export PATH="/s/testbin:$PATH"
export AWG_CONF_DIR=/s/testetc/amnezia/amnezia-wg
mkdir -p "$AWG_CONF_DIR" testbin
cat > testbin/awg <<'EOF'
#!/bin/sh
echo "awg $@" >> /s/awg.log
EOF
chmod +x testbin/awg
cat > "$AWG_CONF_DIR/wg0.conf" <<'EOF'
[Interface]
Address = 10.8.1.1/24
ListenPort = 51820
PrivateKey = SRVPRIV

[Peer]
PublicKey = OLDPUB
AllowedIPs = 10.8.1.2/32
EOF

PUB="rH2Y2eM9sQmVieDzS0jLxV8F7pKqZgWn4TcBbUuA1iE="
rm -f /s/awg.log

./awg-peer-add wg0 "$PUB" 10.8.1.5
grep -q "PublicKey = $PUB" "$AWG_CONF_DIR/wg0.conf"
grep -q "AllowedIPs = 10.8.1.5/32" "$AWG_CONF_DIR/wg0.conf"
grep -q "awg set wg0 peer $PUB allowed-ips 10.8.1.5/32" /s/awg.log

if ./awg-peer-add wg0 "$PUB" 10.8.1.6; then echo "dup add must fail" >&2; exit 1; fi

./awg-peer-remove wg0 "$PUB"
! grep -q "PublicKey = $PUB" "$AWG_CONF_DIR/wg0.conf"
grep -q OLDPUB "$AWG_CONF_DIR/wg0.conf"
grep -q "awg set wg0 peer $PUB remove" /s/awg.log

./awg-health wg0
echo "ALL SCRIPT TESTS PASSED"
```

- [ ] **Step 2: Прогнать тест скриптов в alpine**

```bash
chmod +x deploy/server/awg-peer-add deploy/server/awg-peer-remove deploy/server/awg-health deploy/server/test_scripts.sh
docker run --rm -v "$PWD/deploy/server:/s" alpine sh /s/test_scripts.sh
```
Expected: `ALL SCRIPT TESTS PASSED`

- [ ] **Step 3: sudoers + README сервера**

`deploy/server/amnezia-bot.sudoers`:
```
amnezia-bot ALL=(root) NOPASSWD: /usr/local/bin/awg-peer-add, /usr/local/bin/awg-peer-remove, /usr/local/bin/awg-health
```

`deploy/server/README.md`:
```markdown
# Подготовка AmneziaWG-сервера

1. Создать пользователя и ключ:
       useradd -m -s /bin/sh amnezia-bot
       # с машины бота: ssh-copy-id -i <bot_key> amnezia-bot@SERVER
2. Установить скрипты:
       install -m 755 awg-peer-add awg-peer-remove awg-health /usr/local/bin/
       install -m 440 -o root -g root amnezia-bot.sudoers /etc/sudoers.d/amnezia-bot
3. Права на конфиг: файл /etc/amnezia/amnezia-wg/wg0.conf принадлежит root:root 600
   (скрипты пишут в него через sudo).
4. Убедиться, что `sudo -u amnezia-bot sudo awg-health wg0` возвращает ok.

## IPv6
Бот выдаёт только IPv4-конфиги. Чтобы клиентский IPv6-трафик не обходил туннель,
заблокируйте IPv6 на сервере (ip6tables -P FORWARD DROP или отключите v6 на VPS)
и/или отключайте IPv6 на клиентах.

## Риски
- HostKey SSH сейчас не пинится (InsecureIgnoreHostKey). Для прода добавьте
  known_hosts в образ или реализуйте pinning.
- Скрипты валидируют аргументы; sudoers разрешает только эти три команды.
```

- [ ] **Step 4: Dockerfile**

`deploy/docker/Dockerfile`:
```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/amnezia-bot ./cmd/bot

FROM alpine:3.20
RUN adduser -D -u 10001 bot
USER bot
COPY --from=build /out/amnezia-bot /usr/local/bin/amnezia-bot
EXPOSE 8080
ENTRYPOINT ["amnezia-bot"]
```

Run: `make docker`
Expected: образ собирается

- [ ] **Step 5: Kubernetes-манифесты**

`deploy/k8s/configmap.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: amnezia-bot-config
  namespace: amnezia
data:
  config.yaml: |
    admin_ids: [111111111]
    default_limit: 3
    routes:
      url: https://raw.githubusercontent.com/w1zardz/amnezia-split-route-sync/master/dist/wg-allowed-ips.txt
      refresh_interval: 1h
    monitor:
      check_interval: 30s
      down_threshold: 2m
    servers:
      - id: spb-1
        display_name: SPB VPN-1
        enabled: true
        host: 203.0.113.10
        ssh_port: 22
        ssh_user: amnezia-bot
        interface: wg0
        endpoint: 203.0.113.10:51820
        server_public_key: "BASE64_PUBKEY_OF_SERVER"
        client_cidr: 10.8.1.0/24
        dns: []
        awg:
          jc: 4
          jmin: 40
          jmax: 70
          s1: 68
          s2: 149
          h1: 1234567
          h2: 2345678
          h3: 3456789
          h4: 4567890
```

`deploy/k8s/secret.example.yaml`:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: amnezia-bot-secrets
  namespace: amnezia
type: Opaque
stringData:
  BOT_TOKEN: "123456:ABC-DEF..."
  DATABASE_URL: "postgres://amnezia:pass@postgres.amnezia.svc:5432/amnezia_bot?sslmode=disable"
  # содержимое приватного SSH-ключа; монтируется файлом id_ed25519
  ssh_private_key: |
    -----BEGIN OPENSSH PRIVATE KEY-----
    ...
    -----END OPENSSH PRIVATE KEY-----
```

`deploy/k8s/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: amnezia-bot
  namespace: amnezia
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: amnezia-bot
  template:
    metadata:
      labels:
        app: amnezia-bot
    spec:
      containers:
        - name: bot
          image: registry.example.com/amnezia-bot:0.1.0
          args: ["-config", "/etc/amnezia-bot/config.yaml"]
          env:
            - name: BOT_TOKEN
              valueFrom:
                secretKeyRef: { name: amnezia-bot-secrets, key: BOT_TOKEN }
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef: { name: amnezia-bot-secrets, key: DATABASE_URL }
            - name: SSH_PRIVATE_KEY
              value: /etc/amnezia-bot/ssh/id_ed25519
          volumeMounts:
            - { name: config, mountPath: /etc/amnezia-bot, readOnly: true }
            - { name: ssh-key, mountPath: /etc/amnezia-bot/ssh, readOnly: true }
          ports:
            - { containerPort: 8080 }
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { memory: 128Mi }
      volumes:
        - name: config
          configMap: { name: amnezia-bot-config }
        - name: ssh-key
          secret:
            secretName: amnezia-bot-secrets
            items:
              - { key: ssh_private_key, path: id_ed25519 }
```

- [ ] **Step 6: Коммит**

```bash
git add -A && git commit -m "feat: docker, k8s manifests and server-side peer scripts"
```

---

### Task 17: Финальная проверка

**Files:**
- Modify: при необходимости `README.md`

- [ ] **Step 1: Статические проверки**

```bash
gofmt -l . | tee /dev/stderr | (! read)   # пустой вывод = OK
go vet ./...
golangci-lint run   # при отсутствии: brew install golangci-lint
```
Expected: без ошибок (найденное исправить и закоммитить `chore: fix lint`).

- [ ] **Step 2: Полные тесты**

```bash
make test
make test-integration
```
Expected: PASS

- [ ] **Step 3: Самопроверка по Acceptance criteria (спека §11)**

Прогнать чек-лист вручную на staging (реальный сервер + реальный бот). Каждый пункт — отдельная ручная операция:

1. Неизвестный Telegram ID: `/start` → «Нет доступа.» (AC-1).
2. Админ: `/adduser <id>`, `/setlimit <id> 2`, `/disableuser <id>` (AC-2).
3. Пользователь создаёт конфиги до лимита; превышение лимита → отказ (AC-3).
4. На сервере: `sudo awg show wg0` показывает peer с ключом и IP (AC-4).
5. `.conf` импортируется в AmneziaWG-клиент и подключается (AC-5).
6. В выданном конфиге `AllowedIPs` — список «весь IPv4, кроме RU и приватных» (AC-6).
7. «Мои устройства» показывает только свои; удаление работает (AC-7).
8. После удаления peer не подключается (AC-8).
9. `kubectl rollout restart` — пользователи и метаданные на месте (AC-9).
10. `SELECT * FROM vpn_peers` — только публичные ключи; в логах нет конфигов (AC-10).
11. Остановить SSH-доступ на порог → один алерт-апдейт (edit того же сообщения), восстановление → edit «работает»; обращение пользователя → карточка обновляется (AC-11).

- [ ] **Step 4: Финальный коммит**

```bash
git add -A && git commit -m "chore: final verification fixes" || true
```

---

## Execution Notes

- Порядок задач фиксированный: 1 → 17.
- Каждый шаг «Run: ...» обязателен к выполнению с проверкой ожидаемого результата.
- Если шаг падает не так, как ожидалось — остановиться и разобраться, не продолжать по инерции.
