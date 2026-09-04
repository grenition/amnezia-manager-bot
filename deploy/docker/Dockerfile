FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/amnezia-bot ./cmd/bot

FROM alpine:3.20
RUN adduser -D -u 10001 bot
USER bot
COPY --from=build /out/amnezia-bot /usr/local/bin/amnezia-bot
EXPOSE 8080
ENTRYPOINT ["amnezia-bot"]
