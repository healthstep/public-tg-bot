FROM golang:1.25-alpine AS build
WORKDIR /build

RUN apk update && apk add --no-cache \
    git \
    gcc \
    musl-dev

ARG GITHUB_TOKEN
RUN echo "machine github.com login porebric password ${GITHUB_TOKEN}" > /root/.netrc && chmod 600 /root/.netrc

ENV GOPRIVATE=github.com

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
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /app/public-tg-bot ./cmd/public-tg-bot

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /app/public-tg-bot .
COPY --from=build /build/public-tg-bot/config/configs_keys.yml ./config/configs_keys.yml
EXPOSE 8081
ENV APP_ENV=production
CMD ["./public-tg-bot"]
