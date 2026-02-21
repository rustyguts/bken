# Resource Planning

BKEN is designed for small communities: friend groups, gaming guilds, small teams. This page documents the server resource requirements to help you choose appropriate hardware.

## Server Resources

### CPU

The server does not process or transcode audio. It relays WebSocket control messages and WebRTC signaling, and serves the REST API. Audio flows peer-to-peer between clients. Server-side recording adds a small overhead for muxing incoming Opus packets to OGG.

- **Base**: negligible (idle server uses <1% of a single core)
- **Per voice user**: ~0.01 core (signaling and control only)
- **REST API**: ~0.05 core under moderate upload/download traffic

### Memory

- **Base**: ~30 MB (Go runtime + SQLite + HTTP server)
- **Per connected user**: ~1 MB (WebSocket connection, control state, in-memory channel data)
- **File uploads**: uploaded files are streamed to disk; memory usage is bounded by request size (max 10 MB)

### Bandwidth

Audio travels peer-to-peer between clients and does not pass through the server. Server bandwidth is consumed by:

- **WebSocket signaling**: ~1 KB/s per user (control messages, ping/pong, chat)
- **File uploads/downloads**: depends on usage; max 10 MB per upload
- **WebRTC signaling**: brief bursts during connection setup (SDP offers/answers, ICE candidates)

Client-to-client audio bandwidth (for network planning):

- **Voice (Opus)**: 32 kbps per stream (adaptive: 8-48 kbps based on conditions)
- **Per user in a channel**: sends 1 stream, receives N-1 streams (peer-to-peer mesh)
- **Example**: 5 users in a voice channel = each client sends 32 kbps and receives 4 x 32 = 128 kbps

### Disk

- **SQLite database**: typically <1 MB (settings, channels, files, bans, audit log)
- **Blob bytes (uploads, images, videos, recordings)**: depends on usage; stored in `blobs/` directory next to the database (or custom `-blobs-dir`)
- **Blob metadata**: stored in SQLite (names, types, sizes, UUID disk names)
- **Logs**: standard output, no log files by default

### Example Configurations

| Scenario | Users | Server | Notes |
|----------|-------|--------|-------|
| LAN party | 5-10 | Any machine on the network | A Raspberry Pi 4 works fine |
| Small team | 10-25 | 1 vCPU, 512 MB RAM | Cheapest VPS tier |
| Gaming guild | 25-50 | 1 vCPU, 1 GB RAM | Standard VPS |
| Community server | 50-100 | 2 vCPU, 2 GB RAM | Most server bandwidth is chat/signaling |

## Capacity Limits

### Recommended Limits

- **Per channel**: up to 25 simultaneous voice users (peer-to-peer mesh; each client maintains N-1 WebRTC connections)
- **Per server**: up to 500 connected clients (hard limit); 10 connections per IP address
- **Rate limiting**: 50 control messages per second per client
- **Chat messages**: 500 characters max per message; message ownership tracked for up to 10,000 recent messages

### Why These Limits?

BKEN uses a peer-to-peer WebRTC mesh for audio. Each user in a voice channel maintains a direct connection to every other user. The number of connections grows as N x (N-1) / 2, which means:

| Users | Connections | Client bandwidth (32 kbps) |
|-------|-------------|---------------------------|
| 2 | 1 | 32 kbps each |
| 5 | 10 | 128 kbps each |
| 10 | 45 | 288 kbps each |
| 25 | 300 | 768 kbps each |

Above 25 users per channel, client CPU and bandwidth become the bottleneck, not the server.

## Scaling Notes

BKEN is intentionally designed for small communities. It is not a replacement for Discord, Slack, or Zoom at scale.

- **Horizontal scaling**: not supported. Each server is an independent channel.
- **Persistence**: chat messages are ephemeral (in-memory). Only settings, channels, and file metadata are persisted to SQLite.
- **Backups**: copy the `bken.db` SQLite file and the `blobs/` directory.
