FROM alpine:3.20
RUN apk add --no-cache openssh sudo
RUN adduser -D amnezia-bot \
    && sed -i 's/^amnezia-bot:!:/amnezia-bot:*:/' /etc/shadow
COPY awg-stub /usr/local/bin/awg
COPY entrypoint.sh /entrypoint.sh
RUN chmod 755 /usr/local/bin/awg /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
