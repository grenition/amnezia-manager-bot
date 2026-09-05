# amnezia-manager-bot

Telegram-бот для самостоятельной выдачи конфигов AmneziaWG доверенным пользователям.
Доступ — только по числовому Telegram User ID, состояние — в PostgreSQL, управлением
peer'ами на VPN-сервере — по SSH через sudo-скрипты.

Спека: [docs/amnezia-manager-bot_spec.md](docs/amnezia-manager-bot_spec.md)

## Как это работает

1. Пользователь открывает бота → `/start` → главное меню.
2. «Создать конфиг» → вводит имя устройства → бот проверяет лимит, создаёт peer
   на AmneziaWG-сервере, подставляет в конфиг split-routing (весь IPv4, кроме
   российских и приватных сетей) и присылает `.conf` файл.
3. Конфиг выдаётся один раз; приватные ключи и полные конфиги нигде не хранятся.
   Потерянный конфиг нужно удалить и создать заново.
4. «Мои устройства» → список активных конфигов и удаление любого из них.

## Команды

Пользователю доступно меню из `/start`:

- **Создать конфиг** — выдача `.conf` (имя устройства: 3–32 символа, латиница/цифры/`-`/`_`)
- **Мои устройства** — активные конфигы и лимит; удаление с подтверждением
- **Инструкция** — как импортировать конфиг в AmneziaWG
- **Пожаловаться** — обращение уходит администраторам

Администратору (Telegram ID задаётся в `admin_ids` конфига):

```
/adduser <telegram_id> [username]   # добавить пользователя (лимит default_limit, доступ ко всем серверам)
/disableuser <telegram_id>          # отключить и отозвать все его peer'ы
/setlimit <telegram_id> <1..50>     # изменить лимит конфигов
/users                              # список пользователей и активных конфигов
```

## Разработка

```sh
make up              # postgres в docker compose (amnezia_dev + amnezia_test), остаётся запущен
make down            # остановить postgres
make test            # unit-тесты (без внешних сервисов)
make test-integration  # + интеграционные тести (нужен make up)
make lint            # golangci-lint
make build           # bin/amnezia-bot
make docker          # образ amnezia-bot:dev
```

Локальный запуск (после `make up`; конфиг — на основе `configs/config.example.yaml`):

```sh
BOT_TOKEN=... SSH_PRIVATE_KEY=/path/to/ssh/key \
  DATABASE_URL="postgres://postgres:postgres@localhost:54329/amnezia_dev?sslmode=disable" \
  ./bin/amnezia-bot -config configs/config.yaml
```

## Локальное тестирование

В compose поднимается `fake-vpn` — стенд AmneziaWG-сервера: sshd + настоящие
sudo-скрипты из self-infra (`servers/amnezia-vpn`) + заглушка `awg`, которая
пишет вызовы в лог. Бот работает с ним по-настоящему: создание/удаление
peer'ов, health-проверки, алерты. Не подключается только сам туннель.

1. `make up` — postgres + fake-vpn (ssh на `127.0.0.1:2222`), dev-ключ создается `make dev-key`
2. `cp .env.example .env`, вписать `BOT_TOKEN` (из [@BotFather](https://t.me/BotFather))
3. Свой Telegram User ID ([@userinfobot](https://t.me/userinfobot)) — в `admin_ids` в `configs/config.local.yaml`
4. `make run`
5. В Telegram: `/adduser <свой_id> <username>` (админ тоже должен быть в users,
   чтобы создавать конфиги) → «Создать конфиг» и т.д.

Полезное:

```sh
make vpn-state   # wg0.conf (peers) и лог вызовов awg на фейковом сервере
docker exec -it amnezia-postgres psql -U postgres -d amnezia_dev   # содержимое БД
```

Путь к self-infra задаётся через `SELF_INFRA_DIR` (по умолчанию
`../../DocsPlatform/self-infra` относительно репозитория).

## Развёртывание

- **Docker**: `deploy/docker/Dockerfile` (multi-stage, непривилегированный пользователь).
- **Kubernetes и сетап VPN-сервера** — в репозитории `self-infra`:
  `k8s/apps/amnezia-bot/` (манифесты, ConfigMap, secret-схема) и
  `servers/amnezia-vpn/` (sudo-скрипты `awg-peer-add` / `awg-peer-remove` /
  `awg-health`, sudoers, тесты скриптов).

## Архитектура

```
cmd/bot            — wiring, /healthz, graceful shutdown
internal/tgbot     — команды, меню, диалоги, тексты ошибок
internal/service   — доступ, лимиты, создание/удаление конфигов, админ-операции
internal/vpn       — интерфейс провайдера, генерация ключей, шаблон конфига
internal/vpn/sshprovider — SSH к серверу, sudo-скрипты
internal/patcher   — замена AllowedIPs + валидация split-routing списка
internal/routes    — last-known-good список AllowedIPs (встроенный + обновление по URL)
internal/netalloc  — аллокация клиентских IP в подсети сервера
internal/alerts    — статусные карточки серверов для админов (одно сообщение, редактируется)
internal/monitor   — проверка доступности серверов с порогом алерта
internal/store     — контракт хранилища; memory (тесты) и postgres (pgx + goose)
```

## Безопасность

- Доступ и админство — только по числовым Telegram User ID (из БД и ConfigMap).
- В БД и логах нет приватных ключей и полных конфигов; конфиг выдаётся один раз.
- Секреты (токен, DSN, SSH-ключ) — только через env; ошибки пользователю без
  внутренних деталей.
- Управление сервером — отдельный SSH-пользователь с sudo только на три скрипта.
