# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build

RUN apk update && apk add --no-cache \
    git \
    gcc \
    musl-dev

ARG GITHUB_TOKEN
RUN echo "machine github.com login porebric password ${GITHUB_TOKEN}" > /root/.netrc && chmod 600 /root/.netrc
RUN git config --global url."https://github.com/healthstep/".insteadOf "https://github.com/helthtech/"

ENV GOPRIVATE=github.com/helthtech

WORKDIR /app

COPY public-tg-bot/go.mod public-tg-bot/go.sum ./public-tg-bot/

WORKDIR /app/public-tg-bot
RUN go mod download

WORKDIR /app
COPY public-tg-bot/ ./public-tg-bot/

WORKDIR /app/public-tg-bot
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /out/public-tg-bot ./cmd/public-tg-bot

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/public-tg-bot .
COPY --from=build /app/public-tg-bot/config/configs_keys.yml ./config/configs_keys.yml
EXPOSE 8081
ENV APP_ENV=production
CMD ["./public-tg-bot"]
