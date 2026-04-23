// Paths shared between the Nuxt server and the Python pipeline.
//
// The Python side writes `clip_path` and `thumb_path` as paths relative to
// `DATA_DIR`. We resolve them here the same way `bken.config.resolve_data`
// does in Python, so a DB populated in a host venv keeps working inside the
// Docker container (where DATA_DIR is `/app/data`).

import { existsSync } from 'node:fs'
import { isAbsolute, resolve, sep } from 'node:path'

// `BKEN_DATA` is set by docker-compose; fall back to `../data` so `bun
// run dev` on the host finds the same SQLite + mp4s without configuration.
export const DATA_DIR = resolve(process.env.BKEN_DATA || resolve(process.cwd(), '../data'))

// `BKEN_DB_PATH` lets the SQLite file (plus its -wal/-shm sidecars) live in
// a named docker volume so `docker compose down -v` wipes it without
// touching the host-bound `./data/` (clips, thumbs, libraries).
export const DB_PATH = process.env.BKEN_DB_PATH
  ? resolve(process.env.BKEN_DB_PATH)
  : resolve(DATA_DIR, 'index.db')
export const THUMBS_DIR = resolve(DATA_DIR, 'thumbs')

/**
 * Resolve a clip/thumb path from the DB to an absolute file on disk.
 *
 *   - relative paths → joined onto DATA_DIR
 *   - absolute paths under DATA_DIR → returned as-is
 *   - absolute paths on another host → re-rooted by finding `/data/`
 *
 * Returns the path even if the file doesn't exist; the caller 404s.
 */
export function resolveData(stored: string): string {
  if (!stored) return ''
  if (!isAbsolute(stored)) return resolve(DATA_DIR, stored)
  if (existsSync(stored)) return stored
  // Strip everything up to and including the *last* `data` segment and
  // rebuild under our DATA_DIR.
  const parts = stored.split(sep)
  for (let i = parts.length - 1; i >= 0; i--) {
    if (parts[i] === 'data') {
      return resolve(DATA_DIR, ...parts.slice(i + 1))
    }
  }
  return stored
}
