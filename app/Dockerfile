# Nuxt image — Bun runtime, ffmpeg for on-demand thumbnail rendering.
#
# The server routes in `nuxt/server/` use `better-sqlite3` (native addon)
# and shell out to `ffmpeg` for the `/thumb/:id.jpg` endpoint. Both need
# to be available inside the container.

FROM oven/bun:1.3

# ffmpeg + libsndfile kept for parity with the backend image; only ffmpeg
# is strictly required by the Nuxt server. Build-essential is there for
# better-sqlite3's prebuilt-gyp fallback if the platform binary is missing.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ffmpeg \
        build-essential \
        python3 \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# --- dep layer (cached across source edits) ----
COPY package.json bun.lock ./
# `bun pm trust` approves build scripts for native modules (better-sqlite3,
# @parcel/watcher). Trusted-deps list is already in package.json; this flag
# actually runs them.
RUN bun install --frozen-lockfile --trust \
    || bun install --frozen-lockfile

# --- project layer ----
COPY ./ ./

EXPOSE 3000

CMD ["bun", "run", "dev"]
