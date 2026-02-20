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
COPY . .
RUN CGO_ENABLED=0 go build -o /server .

# ── prod: minimal runtime image ──────────────────────────────────────────────
FROM alpine:latest AS prod
COPY --from=build /server /server
EXPOSE 4433/udp 4433/tcp
ENTRYPOINT ["/server"]
