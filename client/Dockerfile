# ── base: system deps + Go toolchain + Wails CLI + module cache ───────────────
FROM golang:1-bookworm AS base

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
    autoconf automake libtool git \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# rnnoise is not in Debian Bookworm main — build from source.
RUN git clone --depth=1 https://github.com/xiph/rnnoise /tmp/rnnoise \
    && cd /tmp/rnnoise \
    && ./autogen.sh \
    && ./configure --prefix=/usr \
    && make install \
    && rm -rf /tmp/rnnoise

# World-accessible GOPATH so the container can run as the host user.
ENV GOPATH=/go
ENV PATH=$PATH:/go/bin

RUN go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Pre-warm the Go module cache (lives in /go/pkg/mod, unaffected by /app mount).
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download all

# Make the Go toolchain and module cache fully accessible to any user.
RUN chmod -R a+rwX /go

# Pre-create dirs used as anonymous volumes so they're world-writable when
# Docker initialises the volume from the image (before bind mounts apply).
RUN mkdir -p /app/frontend/node_modules /app/frontend/dist /app/frontend/wailsjs/runtime \
    && touch /app/frontend/dist/.gitkeep \
    && chmod -R a+rwx /app/frontend

# ── dev: wails dev with source mounted at /app ────────────────────────────────
FROM base AS dev
CMD ["sh", "-c", "cd /app/frontend && npm install && cd /app && wails dev -tags webkit2_41"]

# ── build: compile Wails desktop binary ──────────────────────────────────────
FROM base AS build
COPY . .
RUN cd frontend && npm install
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
# rnnoise shared lib from the build stage (not in bookworm-slim repos).
COPY --from=build /usr/lib/librnnoise* /usr/lib/
ENTRYPOINT ["/client"]
