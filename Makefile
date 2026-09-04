GO ?= go
BIN := bin/amnezia-bot

.PHONY: build test test-integration lint routes-download docker up down

build:
	$(GO) build -o $(BIN) ./cmd/bot

test:
	$(GO) test ./...

up:
	docker compose up -d --wait

down:
	docker compose down

test-integration:
	@docker compose ps --status running -q postgres | grep -q . || { echo "postgres is down: run 'make up' first" >&2; exit 1; }
	TEST_POSTGRES=1 TEST_DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_test?sslmode=disable" $(GO) test ./...

lint:
	golangci-lint run

routes-download:
	curl -fsSL "$(URL)" -o internal/routes/assets/allowed_ips_default.txt

docker:
	docker build -f deploy/docker/Dockerfile -t amnezia-bot:dev .
