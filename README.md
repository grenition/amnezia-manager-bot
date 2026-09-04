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
