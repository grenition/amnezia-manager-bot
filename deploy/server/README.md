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
