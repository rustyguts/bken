FROM fedora:43 AS dev

WORKDIR /app

ENV GOPATH=/go \
    GOCACHE=/go/cache \
    PATH="/go/bin:/root/.bun/bin:${PATH}" \
    DISPLAY=:99

RUN dnf -y update && \
    dnf -y install \
      unzip \
      golang \
      git \
      gcc \
      gcc-c++ \
      make \
      pkgconf-pkg-config \
      webkit2gtk4.1-devel \
      gtk3-devel \
      xorg-x11-server-Xvfb \
      portaudio-devel \
      opus-devel \
      opusfile-devel \
      curl \
      ca-certificates && \
    dnf clean all

RUN curl -fsSL https://bun.sh/install | bash

# Wails dev browser mode calls pkg/browser, which expects one of:
# xdg-open, x-www-browser, or www-browser.
# In containers we don't want to launch a local browser, so provide no-op
# executables to satisfy that check and keep the process alive.
RUN printf '#!/bin/sh\nexit 0\n' > /usr/local/bin/xdg-open && \
    chmod +x /usr/local/bin/xdg-open && \
    ln -sf /usr/local/bin/xdg-open /usr/local/bin/x-www-browser && \
    ln -sf /usr/local/bin/xdg-open /usr/local/bin/www-browser

COPY go.mod go.sum ./
RUN go mod download

RUN go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0

COPY frontend/package.json frontend/bun.lock ./frontend/
RUN cd frontend && bun install --frozen-lockfile

COPY . .

EXPOSE 5173

ENTRYPOINT ["sh", "-lc", "mkdir -p /tmp/runtime-root && chmod 700 /tmp/runtime-root && export XDG_RUNTIME_DIR=/tmp/runtime-root && Xvfb :99 -screen 0 1280x720x24 -ac +render -noreset >/tmp/xvfb.log 2>&1 & exec wails dev -browser -noreload -nogorebuild -nosyncgomod -devserver 0.0.0.0:5173 -tags webkit2_41"]
