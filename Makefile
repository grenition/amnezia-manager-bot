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
