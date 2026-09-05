FROM alpine:3.20
RUN apk add --no-cache openssh sudo busybox-extras
RUN adduser -D amnezia-bot \
    && sed -i 's/^amnezia-bot:!:/amnezia-bot:*:/' /etc/shadow
COPY deploy/docker/fake-vpn/awg-stub /usr/local/bin/awg
COPY deploy/server/awg-peer-add deploy/server/awg-peer-remove deploy/server/awg-health /usr/local/bin/
COPY deploy/server/amnezia-bot.sudoers /etc/sudoers.d/amnezia-bot
RUN chmod 755 /usr/local/bin/awg /usr/local/bin/awg-* \
    && chmod 440 /etc/sudoers.d/amnezia-bot
COPY deploy/docker/fake-vpn/entrypoint.sh /entrypoint.sh
COPY deploy/docker/fake-vpn/www /www
RUN chmod 755 /entrypoint.sh /www/cgi-bin/peers
EXPOSE 22 80
ENTRYPOINT ["/entrypoint.sh"]
