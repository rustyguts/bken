// Single-file SQLite connection shared across all server routes.
//
// `better-sqlite3` is synchronous — fine for the workload (single-user
// admin panel, tiny queries). We open WAL mode for concurrent reader/writer
// safety while the Python pipeline may be writing.

// Uses Bun's built-in `bun:sqlite` — we run the Nuxt server under the Bun
// runtime, and Bun's node-api compat layer can't load `better-sqlite3`'s
// native addon (as of Bun 1.3 — see https://github.com/oven-sh/bun/issues/4290).
// The `bun:sqlite` API is a near-drop-in replacement.
//
// `@ts-expect-error` on the import keeps tsc/Nuxt's type-check happy — the
// `bun:sqlite` types ship with Bun and aren't in Nuxt's tsconfig by default.

// @ts-expect-error bun:sqlite is a Bun runtime module
import { Database } from 'bun:sqlite'
import { basename, parse } from 'node:path'
import { DB_PATH } from './paths'

type DatabaseType = InstanceType<typeof Database>

let _db: DatabaseType | null = null

// Forward-only, idempotent ALTER TABLE migrations — mirrors the list in
// `src/vidpipe/db.py::_MIGRATIONS`. Either side can open the DB first, so
// both need to apply any missing columns on connect.
const MIGRATIONS: Array<[table: string, column: string, ddl: string]> = [
  ['video', 'game', 'ALTER TABLE video ADD COLUMN game TEXT'],
  ['video', 'recorded_at', 'ALTER TABLE video ADD COLUMN recorded_at TEXT'],
  ['candidate_clip', 'llm_contents', 'ALTER TABLE candidate_clip ADD COLUMN llm_contents TEXT'],
  ['candidate_clip', 'thumb_path', 'ALTER TABLE candidate_clip ADD COLUMN thumb_path TEXT'],
  ['candidate_clip', 'user_rating', 'ALTER TABLE candidate_clip ADD COLUMN user_rating INTEGER'],
  ['candidate_clip', 'active', 'ALTER TABLE candidate_clip ADD COLUMN active INTEGER NOT NULL DEFAULT 1'],
  ['video', 'vision_done', 'ALTER TABLE video ADD COLUMN vision_done INTEGER NOT NULL DEFAULT 0'],
  ['candidate_clip', 'title', 'ALTER TABLE candidate_clip ADD COLUMN title TEXT'],
  ['candidate_clip', 'source', "ALTER TABLE candidate_clip ADD COLUMN source TEXT NOT NULL DEFAULT 'auto'"],
  ['job', 'type', 'ALTER TABLE job ADD COLUMN type TEXT'],
  ['video', 'library_id', 'ALTER TABLE video ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE SET NULL'],
  ['video', 'missing', 'ALTER TABLE video ADD COLUMN missing INTEGER NOT NULL DEFAULT 0'],
  ['video', 'sha1_full', 'ALTER TABLE video ADD COLUMN sha1_full TEXT'],
  ['job', 'library_id', 'ALTER TABLE job ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE CASCADE'],
  ['candidate_clip', 'clip_stale', 'ALTER TABLE candidate_clip ADD COLUMN clip_stale INTEGER NOT NULL DEFAULT 0'],
]

function migrate(conn: DatabaseType): void {
  // Ensure the library table exists
  conn.exec(`
    CREATE TABLE IF NOT EXISTS library (
      id                   INTEGER PRIMARY KEY,
      name                 TEXT NOT NULL,
      path                 TEXT NOT NULL UNIQUE,
      scan_interval_minutes INTEGER NOT NULL DEFAULT 60,
      last_scan_at         TEXT,
      next_scan_at         TEXT,
      active               INTEGER NOT NULL DEFAULT 1,
      created_at           TEXT DEFAULT (datetime('now'))
    )
  `)
  conn.exec('CREATE INDEX IF NOT EXISTS idx_library_path ON library(path)')
  conn.exec('CREATE INDEX IF NOT EXISTS idx_library_active ON library(active)')

  // Ensure the job table exists — older DBs created before the job queue
  // was introduced won't have it, and `ALTER TABLE job ...` below would fail.
  conn.exec(`
    CREATE TABLE IF NOT EXISTS job (
      id          INTEGER PRIMARY KEY,
      video_id    INTEGER REFERENCES video(id) ON DELETE CASCADE,
      library_id  INTEGER REFERENCES library(id) ON DELETE CASCADE,
      type        TEXT NOT NULL,
      status      TEXT NOT NULL DEFAULT 'pending',
      target_id   INTEGER,
      error_message TEXT,
      created_at  TEXT DEFAULT (datetime('now')),
      updated_at  TEXT DEFAULT (datetime('now'))
    )
  `)

  for (const [table, column, ddl] of MIGRATIONS) {
    const cols = conn.query(`PRAGMA table_info(${table})`).all() as { name: string }[]
    if (!cols.some((c) => c.name === column)) {
      conn.exec(ddl)
    }
  }

  conn.exec('CREATE INDEX IF NOT EXISTS idx_clip_active ON candidate_clip(video_id, active)')
  conn.exec(
    'CREATE INDEX IF NOT EXISTS idx_clip_active_source ON candidate_clip(video_id, active, source)',
  )
  conn.exec('CREATE INDEX IF NOT EXISTS idx_job_status ON job(status, updated_at)')
  conn.exec('CREATE INDEX IF NOT EXISTS idx_job_video ON job(video_id, status)')
  conn.exec('CREATE INDEX IF NOT EXISTS idx_job_library ON job(library_id, status)')
  conn.exec('CREATE INDEX IF NOT EXISTS idx_video_library ON video(library_id)')
  conn.exec('CREATE INDEX IF NOT EXISTS idx_video_missing ON video(missing)')
}

export function db(): DatabaseType {
  if (!_db) {
    console.log(`[db] opening ${DB_PATH} (cwd=${process.cwd()}, VIDPIPE_DATA=${process.env.VIDPIPE_DATA})`)
    _db = new Database(DB_PATH)
    _db.exec('PRAGMA journal_mode = WAL')
    _db.exec('PRAGMA foreign_keys = ON')
    migrate(_db)
  }
  return _db
}

// Common SELECT: clip row with video metadata joined, shaped for the UI.
export const CLIP_SELECT = `
  SELECT c.*, v.game, v.recorded_at, v.path AS video_path
    FROM candidate_clip c
    JOIN video v ON v.id = c.video_id
`

export interface ClipRow {
  id: number
  video_id: number
  start_s: number
  end_s: number
  score: number
  features: string | null
  transcript: string | null
  llm_rank: number | null
  llm_title: string | null
  llm_desc: string | null
  llm_contents: string | null
  llm_tags: string | null
  clip_path: string | null
  thumb_path: string | null
  user_rating: number | null
  active: number | null
  title: string | null
  source: string
  game: string | null
  recorded_at: string | null
  video_path: string | null
}

export interface VideoRow {
  id: number
  path: string
  size_bytes: number
  mtime: number
  sha1_prefix: string | null
  duration_s: number | null
  width: number | null
  height: number | null
  fps: number | null
  video_codec: string | null
  audio_codec: string | null
  audio_channels: number | null
  audio_rate: number | null
  game: string | null
  recorded_at: string | null
  audio_done: number
  transcribe_done: number
  events_done: number
  vision_done: number
  score_done: number
  rank_done: number
  created_at: string
  updated_at: string
}

export interface JobRow {
  id: number
  video_id: number | null
  type: string
  status: string
  target_id: number | null
  error_message: string | null
  created_at: string
  updated_at: string
}

export interface DetectionDto {
  label: string
  count: number
}

export interface ClipDto {
  id: number
  video_id: number
  start_s: number
  end_s: number
  duration: number
  score: number
  llm_rank: number | null
  title: string
  reason: string
  contents: string
  transcript: string
  tags: string[]
  rating: number | null
  game: string
  recorded_at: string
  video_name: string
  thumb_url: string
  video_url: string
  download_url: string
  clip_name: string
  ocr_text: string
  top_detections: DetectionDto[]
  caption_sample: string
}

export function enrichClipsWithVision(rows: ClipRow[]): ClipDto[] {
  if (!rows.length) return []
  const ids = rows.map((r) => r.id)
  const placeholders = ids.map(() => '?').join(',')

  // Top-10 unique OCR strings per clip
  const ocrRows = db()
    .prepare(
      `SELECT DISTINCT c.id AS clip_id, o.text
       FROM candidate_clip c
       JOIN vision_ocr o ON o.video_id = c.video_id
         AND o.ts_s BETWEEN c.start_s AND c.end_s
       WHERE c.id IN (${placeholders})
       ORDER BY c.id, o.area_frac DESC`
    )
    .all(...ids) as { clip_id: number; text: string }[]

  const ocrMap = new Map<number, string[]>()
  for (const r of ocrRows) {
    const list = ocrMap.get(r.clip_id) || []
    if (list.length < 10) {
      list.push(r.text)
      ocrMap.set(r.clip_id, list)
    }
  }

  // Peak caption per clip
  const capRows = db()
    .prepare(
      `SELECT c.id AS clip_id, f.caption
       FROM candidate_clip c
       JOIN vision_frame f ON f.video_id = c.video_id
         AND f.ts_s BETWEEN c.start_s AND c.end_s
       WHERE c.id IN (${placeholders})
       GROUP BY c.id
       HAVING ABS(f.ts_s - (c.start_s + c.end_s) / 2) = MIN(ABS(f.ts_s - (c.start_s + c.end_s) / 2))`
    )
    .all(...ids) as { clip_id: number; caption: string | null }[]

  const capMap = new Map<number, string>()
  for (const r of capRows) {
    if (r.caption) capMap.set(r.clip_id, r.caption)
  }

  // Top-5 detections per clip
  const detRows = db()
    .prepare(
      `SELECT c.id AS clip_id, d.label, COUNT(*) AS cnt
       FROM candidate_clip c
       JOIN vision_detection d ON d.video_id = c.video_id
         AND d.ts_s BETWEEN c.start_s AND c.end_s
       WHERE c.id IN (${placeholders})
       GROUP BY c.id, d.label
       ORDER BY c.id, cnt DESC`
    )
    .all(...ids) as { clip_id: number; label: string; cnt: number }[]

  const detMap = new Map<number, DetectionDto[]>()
  for (const r of detRows) {
    const list = detMap.get(r.clip_id) || []
    if (list.length < 5) {
      list.push({ label: r.label, count: r.cnt })
      detMap.set(r.clip_id, list)
    }
  }

  return rows.map((row) => {
    const base = _clipToDtoBase(row)
    return {
      ...base,
      ocr_text: (ocrMap.get(row.id) || []).join(', '),
      top_detections: detMap.get(row.id) || [],
      caption_sample: capMap.get(row.id) || '',
    }
  })
}

function _clipToDtoBase(row: ClipRow): Omit<ClipDto, 'ocr_text' | 'top_detections' | 'caption_sample'> {
  let tags: string[] = []
  if (row.llm_tags) {
    try {
      tags = JSON.parse(row.llm_tags)
    } catch {
      /* malformed, keep empty */
    }
  }
  const clipPath = row.clip_path || ''
  return {
    id: row.id,
    video_id: row.video_id,
    start_s: row.start_s,
    end_s: row.end_s,
    duration: row.end_s - row.start_s,
    score: row.score,
    llm_rank: row.llm_rank,
    title:
      row.llm_title ||
      `Clip @ ${Math.floor(row.start_s / 60)}:${String(Math.floor(row.start_s % 60)).padStart(2, '0')}`,
    reason: row.llm_desc || '',
    contents: row.llm_contents || '',
    transcript: row.transcript || '',
    tags,
    rating: row.user_rating,
    game: row.game || 'Unknown',
    recorded_at: row.recorded_at || '',
    video_name: row.video_path ? parse(row.video_path).name : '',
    thumb_url: `/thumb/${row.id}.jpg`,
    video_url: `/clip/${row.id}.mp4`,
    download_url: `/clip/${row.id}.mp4?download=1`,
    clip_name: clipPath ? basename(clipPath) : '',
  }
}

export function clipToDto(row: ClipRow): ClipDto {
  const base = _clipToDtoBase(row)
  return {
    ...base,
    ocr_text: '',
    top_detections: [],
    caption_sample: '',
  }
}
