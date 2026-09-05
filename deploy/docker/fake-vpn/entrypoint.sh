#!/bin/sh
set -eu

if [ ! -f /vpn-scripts/awg-peer-add ]; then
	echo "ERROR: /vpn-scripts is empty — mount servers/amnezia-vpn from self-infra" >&2
	exit 1
fi
install -m 755 /vpn-scripts/awg-peer-add /usr/local/bin/
install -m 755 /vpn-scripts/awg-peer-remove /usr/local/bin/
install -m 755 /vpn-scripts/awg-health /usr/local/bin/
install -m 440 -o root -g root /vpn-scripts/amnezia-bot.sudoers /etc/sudoers.d/amnezia-bot

mkdir -p /var/log /etc/amnezia/amnezia-wg
touch /var/log/awg.log
chmod 666 /var/log/awg.log

if [ ! -f /etc/amnezia/amnezia-wg/wg0.conf ]; then
	cat > /etc/amnezia/amnezia-wg/wg0.conf <<'EOF'
[Interface]
Address = 10.8.1.1/24
ListenPort = 51820
PrivateKey = FAKE_SERVER_PRIVATE_KEY_FOR_LOCAL_TESTS
EOF
fi
chmod 600 /etc/amnezia/amnezia-wg/wg0.conf

mkdir -p /home/amnezia-bot/.ssh
if [ -f /keys/id_ed25519.pub ]; then
	install -m 600 -o amnezia-bot -g amnezia-bot /keys/id_ed25519.pub /home/amnezia-bot/.ssh/authorized_keys
else
	echo "WARNING: /keys/id_ed25519.pub not found — run 'make dev-key' and restart" >&2
fi
chown -R amnezia-bot:amnezia-bot /home/amnezia-bot/.ssh

ssh-keygen -A
exec /usr/sbin/sshd -D -e
