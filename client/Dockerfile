# ── base: system deps + Go toolchain + Wails CLI + module cache ───────────────
FROM golang:1-bookworm AS base

# Build-time UID/GID so pre-created dirs are owned by the container user.
# Match the values passed via docker-compose build.args (default: 1000).
ARG UID=1000
ARG GID=1000

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl gnupg \
    libgtk-3-dev \
    libwebkit2gtk-4.1-dev \
    portaudio19-dev \
    libopus-dev \
    libopusfile-dev \
    pkg-config \
    gcc g++ \
    libasound2-dev \
    libpipewire-0.3-dev \
    pipewire-alsa \
    pipewire-jack \
    unzip \
    && rm -rf /var/lib/apt/lists/*

# Node 22 via NodeSource — Bookworm's Node 18 is too old for @tailwindcss/oxide.
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# World-accessible GOPATH so the container can run as the host user.
ENV GOPATH=/go
ENV PATH=$PATH:/go/bin

RUN go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Pre-warm the Go module cache (lives in /go/pkg/mod, unaffected by /app mount).
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download all

# Make the Go toolchain and module cache fully accessible to any user.
RUN chown -R ${UID}:${GID} /go && chmod -R a+rwX /go

# Pre-create dirs that will be shadowed by anonymous volumes.
# chown to the runtime user so Docker initialises the volumes with correct
# ownership — the container user can then chmod/write them freely.
RUN mkdir -p \
        /app/frontend/node_modules \
        /app/frontend/dist \
        /app/frontend/wailsjs/runtime \
        /app/build/bin \
    && touch /app/frontend/dist/.gitkeep \
    && chown -R ${UID}:${GID} /app/frontend /app/build \
    && chmod -R a+rwx /app/frontend /app/build

# ── dev: wails dev with source mounted at /app ────────────────────────────────
FROM base AS dev
CMD ["sh", "-c", "cd /app/frontend && bun install && cd /app && wails dev -tags webkit2_41"]

# ── build: compile Wails desktop binary ──────────────────────────────────────
FROM base AS build
COPY . .
RUN cd frontend && bun install
RUN wails build -tags webkit2_41

# ── prod: slim runtime with only shared libraries ─────────────────────────────
FROM debian:bookworm-slim AS prod

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgtk-3-0 \
    libwebkit2gtk-4.1-0 \
    libportaudio2 \
    libopus0 \
    libopusfile0 \
    libasound2t64 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/build/bin/client /client
ENTRYPOINT ["/client"]
