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

PUB2="rH2Y2eM9sQmVieDzS0jLxV8F7pKqZgWn4TcBbUuA1iF="
./awg-peer-add wg0 "$PUB2" 10.8.1.9/32
grep -q "PublicKey = $PUB2" "$AWG_CONF_DIR/wg0.conf"
grep -q "AllowedIPs = 10.8.1.9/32" "$AWG_CONF_DIR/wg0.conf"
grep -q "awg set wg0 peer $PUB2 allowed-ips 10.8.1.9/32" /s/awg.log

if ./awg-peer-add wg0 "$PUB2" 10.8.1.10/24; then echo "/24 suffix must fail" >&2; exit 1; fi

./awg-peer-remove wg0 "$PUB2"
! grep -q "PublicKey = $PUB2" "$AWG_CONF_DIR/wg0.conf"

PUBPLUS="aB+C/dEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfG="
./awg-peer-add wg0 "$PUBPLUS" 10.8.1.11
grep -q "PublicKey = $PUBPLUS" "$AWG_CONF_DIR/wg0.conf"
st=0
./awg-peer-add wg0 "$PUBPLUS" 10.8.1.12 2>/dev/null || st=$?
[ "$st" -eq 3 ] || { echo "dup add of +/ key must exit 3 (got $st)" >&2; exit 1; }
./awg-peer-remove wg0 "$PUBPLUS"
! grep -q "PublicKey = $PUBPLUS" "$AWG_CONF_DIR/wg0.conf"
grep -q OLDPUB "$AWG_CONF_DIR/wg0.conf"

./awg-peer-remove wg0 "$PUB"
! grep -q "PublicKey = $PUB" "$AWG_CONF_DIR/wg0.conf"
grep -q OLDPUB "$AWG_CONF_DIR/wg0.conf"
grep -q "awg set wg0 peer $PUB remove" /s/awg.log

./awg-health wg0
echo "ALL SCRIPT TESTS PASSED"
