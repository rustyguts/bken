# Download

Pre-built binaries are attached to every push to `main` as [GitHub Actions artifacts](https://github.com/rustyguts/bken/actions/workflows/build.yml).

## Client

The client is the desktop app you talk through. Pick your platform.

### Linux

1. Download **`client-linux-amd64`** from the latest [build](https://github.com/rustyguts/bken/actions/workflows/build.yml).
2. Make it executable and run it:

```bash
chmod +x client-linux-amd64
./client-linux-amd64
```

### macOS

1. Download the **`client-macos-arm64`** archive from the latest [build](https://github.com/rustyguts/bken/actions/workflows/build.yml) and extract it.
2. The binary is not notarised, so clear the quarantine flag before launching:

```bash
xattr -d com.apple.quarantine client
./client
```

### Windows

1. Download **`client-windows-amd64`** and extract `client.exe`.
2. Double-click to run. If Windows Defender SmartScreen blocks it, click **More info → Run anyway**.

---

## Server

One machine on the network runs the relay server. Everyone else connects to it. It handles WebSocket signaling and chat — audio flows peer-to-peer between clients and never passes through the server.

### Linux binary

1. Download **`server-linux-amd64`** from the latest [build](https://github.com/rustyguts/bken/actions/workflows/build.yml).
2. Run it:

```bash
chmod +x server-linux-amd64
./server-linux-amd64
```

The server listens on TCP port **8443** (WebSocket signaling) and **8080** (REST API) by default. To use a different port:

```bash
./server-linux-amd64 -addr :9000
```

### Docker

```bash
docker run --rm -p 8443:8443 -p 8080:8080 -v bken-data:/data ghcr.io/rustyguts/bken-server:latest
```

::: tip Firewall
Open TCP port 8443 on the server machine. WebRTC audio flows directly between clients on ephemeral ports and does not require any additional firewall rules on the server.
:::

---

## Connecting

1. Start the server and note the machine's local IP address (e.g. `192.168.1.10`).
2. Open the client on each machine joining the call.
3. Enter your name and the server address — for example `192.168.1.10:8443` — then click **Connect**.

You will be placed into the first channel automatically. Use the channel list on the left to switch channels or create new ones.

---

## Why does it say "untrusted certificate"?

BKEN generates a self-signed TLS certificate on each server start. The certificate is only used to encrypt the WebSocket connection — it is not used for identity. Clients skip certificate validation by design, which is safe on a trusted local network where you control who can connect.

The fingerprint printed at server startup can be used to manually verify the certificate if needed.
