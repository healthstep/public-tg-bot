FROM golang:1.25-alpine AS builder
WORKDIR /build

RUN apk add --no-cache git

COPY core-users/go.mod core-users/go.sum ./core-users/
COPY core-health/go.mod core-health/go.sum ./core-health/
COPY public-tg-bot/go.mod public-tg-bot/go.sum ./public-tg-bot/

WORKDIR /build/public-tg-bot
RUN go mod download

WORKDIR /build
COPY core-users/ ./core-users/
COPY core-health/ ./core-health/
COPY public-tg-bot/ ./public-tg-bot/

WORKDIR /build/public-tg-bot
RUN CGO_ENABLED=0 go build -o /app/public-tg-bot ./cmd/public-tg-bot

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/public-tg-bot .
COPY --from=builder /build/public-tg-bot/config/configs_keys.yml ./config/configs_keys.yml
EXPOSE 8081
CMD ["./public-tg-bot"]
