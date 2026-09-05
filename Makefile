GO ?= go
BIN := bin/amnezia-bot
DEV_KEY := deploy/docker/fake-vpn/keys/id_ed25519

.PHONY: build test test-integration lint routes-download docker up down dev-key run vpn-state

build:
	$(GO) build -o $(BIN) ./cmd/bot

test:
	$(GO) test ./...

up:
	docker compose up -d --wait --build

down:
	docker compose down

dev-key:
	@mkdir -p deploy/docker/fake-vpn/keys
	@test -f $(DEV_KEY) || ssh-keygen -q -t ed25519 -N "" -C amnezia-bot-dev -f $(DEV_KEY)
	@echo "$(DEV_KEY) готов"

# Локальный запуск бота: BOT_TOKEN обязателен (получить у @BotFather).
run: build dev-key
	@test -n "$$BOT_TOKEN" || { echo "BOT_TOKEN is required (get it from @BotFather)" >&2; exit 1; }
	BOT_TOKEN="$$BOT_TOKEN" SSH_PRIVATE_KEY="$(DEV_KEY)" \
	  DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_dev?sslmode=disable" \
	  ./$(BIN) -config configs/config.local.yaml

# Состояние фейкового VPN-сервера: конфиг peers + лог вызовов awg.
vpn-state:
	@echo "--- wg0.conf ---"
	@docker exec amnezia-fake-vpn cat /etc/amnezia/amnezia-wg/wg0.conf 2>/dev/null || true
	@echo "--- awg.log (последние 20 строк) ---"
	@docker exec amnezia-fake-vpn tail -20 /var/log/awg.log 2>/dev/null || true

test-integration:
	@docker compose ps --status running -q postgres | grep -q . || { echo "postgres is down: run 'make up' first" >&2; exit 1; }
	TEST_POSTGRES=1 TEST_DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_test?sslmode=disable" $(GO) test ./...

lint:
	golangci-lint run

routes-download:
	curl -fsSL "$(URL)" -o internal/routes/assets/allowed_ips_default.txt

docker:
	docker build -f deploy/docker/Dockerfile -t amnezia-bot:dev .
