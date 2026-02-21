# Configuration Reference

The BKEN server is configured entirely via command-line flags. There are no configuration files or environment variables.

## Server CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTPS/WebSocket listen address. Clients connect to this port for signaling. |
| `-api-addr` | `:8080` | REST API listen address. Used for file uploads, health checks, settings. Set to empty string to disable. |
| `-db` | `bken.db` | Path to the SQLite database file. Created on first run. |
| `-idle-timeout` | `30s` | HTTP idle timeout for connections. |
| `-cert-validity` | `24h` | Validity period for the auto-generated self-signed TLS certificate. |
| `-test-user` | *(empty)* | Name for a virtual test bot that emits a 440 Hz tone. Useful for testing audio without a second client. Leave empty to disable. |
| `-turn-url` | *(empty)* | TURN server URL for WebRTC relay (e.g. `turn:turn.example.com:3478`). |
| `-turn-username` | *(empty)* | TURN server username (used with `-turn-url`). |
| `-turn-credential` | *(empty)* | TURN server credential/password (used with `-turn-url`). |

### Examples

**Default (LAN use):**

```bash
./bken-server
```

Listens on `:8080` (signaling) and `:8080` (API), stores data in `./bken.db`.

**Custom ports and data directory:**

```bash
./bken-server \
  -addr :9000 \
  -api-addr :9001 \
  -db /var/lib/bken/bken.db
```

**With TURN server:**

```bash
./bken-server \
  -turn-url "turn:turn.example.com:3478" \
  -turn-username "bken" \
  -turn-credential "secret"
```

**With test bot for audio testing:**

```bash
./bken-server -test-user "ToneBot"
```

A virtual user named "ToneBot" joins the server and emits a continuous 440 Hz sine wave. Useful for verifying audio playback works without a second client.

**Disable REST API:**

```bash
./bken-server -api-addr ""
```

File uploads and the health endpoint will not be available.

## SQLite Database

The database file (default `bken.db`) is created automatically on first run with these tables:

| Table | Purpose |
|-------|---------|
| `schema_migrations` | Tracks applied database migrations |
| `settings` | Key-value store for server settings (name, etc.) |
| `channels` | Voice/text channels (id, name, position) |
| `files` | Metadata for uploaded files (name, content type, disk path, size) |

### First-Run Defaults

On first run, the server seeds:
- A server name of "bken server"
- A "General" channel

### Backups

The SQLite database is a single file. To back up:

```bash
# While server is running (SQLite WAL mode handles concurrent access):
cp bken.db bken.db.backup

# Or use SQLite's backup command for a consistent snapshot:
sqlite3 bken.db ".backup bken.db.backup"
```

Also back up the `uploads/` directory (created next to the database file) if you use file sharing.

## File Uploads

- **Max file size**: 10 MB per upload
- **Storage location**: `uploads/` directory, created next to the database file
- **Naming**: files are stored with UUID filenames to avoid collisions; original names are preserved in the database
- **Endpoint**: `POST /api/upload` (multipart form data with field name `file`)
- **Download**: `GET /api/files/:id` (returns file with `Content-Disposition: attachment`)

## REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check. Returns `{"status":"ok","clients":N}`. |
| `GET` | `/api/room` | Room state: connected users, owner ID. |
| `GET` | `/api/settings` | Server settings (name). |
| `PUT` | `/api/settings` | Update server settings. Body: `{"server_name":"..."}`. |
| `GET` | `/api/channels` | List all channels. |
| `POST` | `/api/channels` | Create a channel. Body: `{"name":"..."}`. |
| `PUT` | `/api/channels/:id` | Rename a channel. Body: `{"name":"..."}`. |
| `DELETE` | `/api/channels/:id` | Delete a channel. |
| `GET` | `/invite` | Browser-friendly invite page. Optional `?addr=host:port` for deep links. |
| `POST` | `/api/upload` | Upload a file (multipart form). |
| `GET` | `/api/files/:id` | Download an uploaded file. |
| `GET` | `/api/recordings` | List server-side recordings. |
| `GET` | `/api/recordings/:id` | Download a recording (OGG/Opus). |
| `GET` | `/api/audit` | Audit log (kick, ban, rename actions). |
| `GET` | `/api/bans` | List active bans. |
| `DELETE` | `/api/bans/:id` | Remove a ban. |

## Wire Protocol Limits

| Limit | Value |
|-------|-------|
| Server/channel/user name length | 50 bytes |
| Chat message length | 500 bytes |
| File upload size | 10 MB |
| Message ownership tracking | 10,000 most recent messages |
