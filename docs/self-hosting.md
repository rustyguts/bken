# Self-Hosting Guide

Run your own BKEN voice server. The server is a single Go binary with no external dependencies beyond SQLite (embedded).

## Quick Start

### Docker

```bash
docker run --rm \
  -p 8443:8443 \
  -p 8080:8080 \
  -v bken-data:/data \
  ghcr.io/rustyguts/bken-server:latest
```

This exposes the WebSocket/TLS signaling port (8443 TCP) and the REST API (8080). Data is persisted in a Docker volume.

### Docker Compose

Create a `docker-compose.yml`:

```yaml
services:
  server:
    image: ghcr.io/rustyguts/bken-server:latest
    ports:
      - "8443:8443"
      - "8080:8080"
    volumes:
      - bken-data:/data
    command: ["-addr", ":8443", "-api-addr", ":8080", "-db", "/data/bken.db"]
    restart: unless-stopped

volumes:
  bken-data:
```

```bash
docker compose up -d
```

### Build from Source

Requires Go 1.22 or later.

```bash
git clone https://github.com/rustyguts/bken.git
cd bken/server
go build -o bken-server .
./bken-server
```

The server listens on `:8443` (TLS/WebSocket) and `:8080` (REST API) by default. See the [Configuration Reference](/configuration) for all flags.

## Network Requirements

BKEN uses two TCP ports:

| Port | Protocol | Purpose |
|------|----------|---------|
| 8443 | TCP | WebSocket signaling (TLS). Clients connect here. |
| 8080 | TCP | REST API for file uploads, health checks, settings. |

WebRTC audio flows peer-to-peer between clients on ephemeral ports — it does not pass through the server. Only the WebSocket signaling connection uses port 8443.

### Firewall Rules

**ufw (Ubuntu/Debian):**

```bash
sudo ufw allow 8443/tcp
sudo ufw allow 8080/tcp
```

**firewalld (Fedora/RHEL):**

```bash
sudo firewall-cmd --permanent --add-port=8443/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

**iptables:**

```bash
iptables -A INPUT -p tcp --dport 8443 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
```

### NAT and STUN/TURN

On a local network (LAN), BKEN works without any NAT configuration. Clients discover each other via the signaling server and establish direct peer-to-peer WebRTC connections.

If clients are on different networks or behind restrictive NATs:

- **STUN** (default): The server includes Google's public STUN server (`stun:stun.l.google.com:19302`). This helps clients discover their public IP addresses. Works for most home and office NATs.
- **TURN** (relay): If direct connections fail (symmetric NAT, corporate firewalls), configure a TURN server to relay media through. See [TURN Setup](#turn-setup) below.

### TURN Setup

Install [coturn](https://github.com/coturn/coturn) on a publicly reachable host:

```bash
# Ubuntu/Debian
sudo apt install coturn
```

Edit `/etc/turnserver.conf`:

```
listening-port=3478
realm=bken.example.com
server-name=bken.example.com
lt-cred-mech
user=bken:your-secret-password
fingerprint
```

Start coturn:

```bash
sudo systemctl enable --now coturn
```

Then pass the TURN server to BKEN:

```bash
./bken-server \
  -turn-url "turn:your-server.example.com:3478" \
  -turn-username "bken" \
  -turn-credential "your-secret-password"
```

### Reverse Proxy

If you want to front BKEN with a reverse proxy, you need WebSocket upgrade support.

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name bken.example.com;

    ssl_certificate     /etc/letsencrypt/live/bken.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/bken.example.com/privkey.pem;

    location /ws {
        proxy_pass https://127.0.0.1:8443;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400s;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        client_max_body_size 11m;
    }
}
```

**Caddy:**

```
bken.example.com {
    handle /ws {
        reverse_proxy https://127.0.0.1:8443 {
            transport http {
                tls_insecure_skip_verify
            }
        }
    }

    handle /api/* {
        reverse_proxy http://127.0.0.1:8080
    }
}
```

### TLS

BKEN generates a self-signed TLS certificate on each start. This is sufficient for LAN use where all participants trust the network.

For production deployments over the internet, place BKEN behind a reverse proxy with a proper TLS certificate (e.g. Let's Encrypt). The WebRTC peer connections between clients are always encrypted regardless of the signaling transport.

The certificate validity defaults to 24 hours and can be adjusted:

```bash
./bken-server -cert-validity 720h  # 30 days
```
