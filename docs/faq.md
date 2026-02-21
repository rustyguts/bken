# FAQ

## General

### What is BKEN?

BKEN is a self-hosted voice chat application for local networks. Think Mumble or TeamSpeak with a modern UI. You run a server on one machine, and everyone connects with the desktop client.

### Do I need an account?

No. There are no accounts, passwords, or email addresses. You pick a username when you connect. Identity is ephemeral -- you can change your name at any time.

### What platforms are supported?

The desktop client runs on Linux, macOS, and Windows. The server runs on any platform that Go supports (Linux, macOS, Windows, FreeBSD, etc.) and is also available as a Docker image.

### Is it free?

Yes. BKEN is open source under the MIT license.

## Server

### How do I start the server?

Download the binary and run it:

```bash
./bken-server
```

Or use Docker:

```bash
docker run --rm -p 8080:8080 ghcr.io/rustyguts/bken-server:latest
```

See the [Self-Hosting Guide](/self-hosting) for full details.

### What ports do I need to open?

- **8080** (TCP): WebSocket signaling and REST API.

WebRTC audio flows peer-to-peer between clients on ephemeral ports and does not pass through the server.

### Can I change the port?

Yes. Use the `-addr` flag:

```bash
./bken-server -addr :9000
```

See the [Configuration Reference](/configuration) for all flags.

### Where is data stored?

In a SQLite database file (default: `bken.db` in the working directory) and an `uploads/` directory for shared files. Chat messages are ephemeral and not persisted to disk.

### How do I back up my server?

Copy the `bken.db` file and the `uploads/` directory. SQLite supports concurrent reads, so you can copy while the server is running.

## Audio

### Why does my audio sound robotic or choppy?

This usually means high packet loss or jitter on the network. Check the connection quality indicator in the bottom bar. BKEN adapts its bitrate automatically (8-48 kbps) based on conditions, but severe network issues will degrade quality.

### How do I use Push-to-Talk?

Open Settings (gear icon) > Keybinds tab > enable Push-to-Talk and set your PTT key. By default, the backtick key (`` ` ``) is used. Hold the key to transmit, release to stop.

### Can I test audio without another person?

Yes. Start the server with the `-test-user` flag:

```bash
./bken-server -test-user "ToneBot"
```

A virtual user named "ToneBot" joins and emits a 440 Hz tone. If you can hear it, your audio pipeline is working.

### What audio codec does BKEN use?

Opus at 48 kHz, mono, with adaptive bitrate (8-48 kbps). Opus is the standard codec for real-time voice and provides excellent quality at low bitrates.

## Networking

### Does BKEN work over the internet?

BKEN is designed for LAN use but can work over the internet if:
1. The signaling server (port 8080) is reachable from all clients
2. Clients can establish peer-to-peer WebRTC connections (may require STUN/TURN)

For internet use, configure a TURN server. See the [Self-Hosting Guide](/self-hosting#turn-setup).

### Can I run with TLS?

Yes. The dev server runs plain WebSocket/HTTP on port `8080`. For TLS, run BKEN behind a reverse proxy (nginx/Caddy/Traefik) with certificates.

### What is STUN/TURN?

- **STUN** helps clients discover their public IP address so they can connect directly (peer-to-peer). Enabled by default.
- **TURN** relays audio through a server when direct connections are not possible (e.g., symmetric NAT, corporate firewalls). Must be configured separately.

## Troubleshooting

### Client can't connect to server

1. Verify the server is running and the address is correct
2. Check that port 8080 is open on the server machine's firewall
3. Make sure both machines are on the same network (for LAN use)
4. Try the server's IP address instead of hostname

### No audio after connecting

1. Check that your microphone is not muted (mic icon in user controls)
2. Verify the correct input/output devices in Settings > Audio
3. Try the mic loopback test in Settings > Audio
4. Check that the other user is also unmuted

### File uploads fail

1. Verify the REST API is running (port 8080 by default)
2. Check that the file is under 10 MB
3. Ensure the `uploads/` directory is writable by the server process
