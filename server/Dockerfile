# ── base: module cache shared by all stages ──────────────────────────────────
FROM golang:1-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# ── dev: hot-reload via Air ───────────────────────────────────────────────────
FROM base AS dev
RUN go install github.com/air-verse/air@latest
ENTRYPOINT ["air"]

# ── build: compile static binary ─────────────────────────────────────────────
FROM base AS build
ARG VERSION=0.1.0-dev
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /server .

# ── prod: minimal runtime image ──────────────────────────────────────────────
FROM alpine:latest AS prod
RUN apk --no-cache add ca-certificates && \
    addgroup -S bken && adduser -S bken -G bken

COPY --from=build /server /server

USER bken
EXPOSE 8080/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/server", "-addr", ":8080"]
