# amnezia-manager-bot

Telegram-бот для самостоятельной выдачи конфигов AmneziaWG доверенным пользователям.
Спека: docs/amnezia-manager-bot_spec.md

## Разработка
    make up              # поднять postgres (docker compose, остаётся запущен)
    make down            # остановить postgres
    make test            # unit-тесты (без внешних сервисов)
    make test-integration  # + интеграционные тесты (нужен make up)
    make lint            # golangci-lint
    make build           # bin/amnezia-bot

Локальный запуск (после make up):
    BOT_TOKEN=... SSH_PRIVATE_KEY=/path/to/key \
      DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_dev?sslmode=disable" \
      ./bin/amnezia-bot -config configs/config.yaml

## Запуск
    BOT_TOKEN=... DATABASE_URL=... SSH_PRIVATE_KEY=/path/to/key \
      ./bin/amnezia-bot -config configs/config.yaml
