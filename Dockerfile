# Multi-stage build for the Go `bken` binary.
#
# Stage 1: build Tailwind+DaisyUI CSS with bun.
# Stage 2: compile the Go binary (no CGo required for default build — ONNX/whisper
#          tags are opt-in and expect libraries provided by the runtime image).
# Stage 3: runtime image — ffmpeg + the binary + embedded static assets.

# ---- css build ----------------------------------------------------------
FROM oven/bun:1 AS cssbuild
WORKDIR /build
COPY package.json ./
RUN bun install
COPY internal/web/ui/static/input.css internal/web/ui/static/input.css
COPY internal/web/ui/templates internal/web/ui/templates
RUN bunx tailwindcss \
      -i internal/web/ui/static/input.css \
      -o internal/web/ui/static/dist/app.css \
      --minify

# ---- go build -----------------------------------------------------------
FROM golang:1.25-alpine AS gobuild
WORKDIR /src
RUN apk add --no-cache git build-base
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overlay the built CSS so //go:embed picks it up.
COPY --from=cssbuild /build/internal/web/ui/static/dist/app.css internal/web/ui/static/dist/app.css

ARG CGO_ENABLED=0
ARG TAGS=""
RUN CGO_ENABLED=${CGO_ENABLED} go build -trimpath -ldflags="-s -w" -tags "${TAGS}" -o /out/bken ./cmd/bken

# ---- dev ----------------------------------------------------------------
FROM golang:1.25-alpine AS dev
WORKDIR /app
RUN apk add --no-cache ffmpeg ca-certificates tzdata git build-base
RUN go install github.com/air-verse/air@latest
COPY . .
COPY --from=cssbuild /build/internal/web/ui/static/dist/app.css internal/web/ui/static/dist/app.css
ENV BKEN_DATA=/app/data \
    BKEN_MODELS=/app/models \
    BKEN_HTTP=:3000
ENTRYPOINT ["air"]

# ---- runtime ------------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /app
COPY --from=gobuild /out/bken /usr/local/bin/bken
ENV BKEN_DATA=/app/data \
    BKEN_MODELS=/app/models \
    BKEN_HTTP=:3000
EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/bken"]
CMD ["serve"]
