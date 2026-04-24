package db

// Schema mirrors cli/src/bken/db.py exactly — same table names, columns,
// indexes. Keeping the DB wire-compatible lets the Go rewrite coexist with
// the Python pipeline during the transition.
const Schema = `
CREATE TABLE IF NOT EXISTS library (
    id                   INTEGER PRIMARY KEY,
    name                 TEXT NOT NULL,
    path                 TEXT NOT NULL UNIQUE,
    scan_interval_minutes INTEGER NOT NULL DEFAULT 60,
    last_scan_at         TEXT,
    next_scan_at         TEXT,
    active               INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_library_path ON library(path);
CREATE INDEX IF NOT EXISTS idx_library_active ON library(active);

CREATE TABLE IF NOT EXISTS video (
    id              INTEGER PRIMARY KEY,
    path            TEXT NOT NULL UNIQUE,
    size_bytes      INTEGER NOT NULL,
    mtime           REAL NOT NULL,
    sha1_prefix     TEXT,
    sha1_full       TEXT,
    duration_s      REAL,
    width           INTEGER,
    height          INTEGER,
    fps             REAL,
    video_codec     TEXT,
    audio_codec     TEXT,
    audio_channels  INTEGER,
    audio_rate      INTEGER,
    game            TEXT,
    recorded_at     TEXT,
    library_id      INTEGER REFERENCES library(id) ON DELETE SET NULL,
    missing         INTEGER NOT NULL DEFAULT 0,
    audio_done      INTEGER NOT NULL DEFAULT 0,
    transcribe_done INTEGER NOT NULL DEFAULT 0,
    events_done     INTEGER NOT NULL DEFAULT 0,
    vision_done     INTEGER NOT NULL DEFAULT 0,
    score_done      INTEGER NOT NULL DEFAULT 0,
    rank_done       INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT DEFAULT (datetime('now')),
    updated_at      TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_video_path ON video(path);
CREATE INDEX IF NOT EXISTS idx_video_library ON video(library_id);
CREATE INDEX IF NOT EXISTS idx_video_missing ON video(missing);

CREATE TABLE IF NOT EXISTS transcript_segment (
    id          INTEGER PRIMARY KEY,
    video_id    INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    start_s     REAL NOT NULL,
    end_s       REAL NOT NULL,
    text        TEXT NOT NULL,
    avg_logprob REAL,
    no_speech   REAL
);
CREATE INDEX IF NOT EXISTS idx_seg_video ON transcript_segment(video_id, start_s);

CREATE TABLE IF NOT EXISTS audio_event (
    id          INTEGER PRIMARY KEY,
    video_id    INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    start_s     REAL NOT NULL,
    end_s       REAL NOT NULL,
    label       TEXT NOT NULL,
    score       REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_evt_video ON audio_event(video_id, start_s);
CREATE INDEX IF NOT EXISTS idx_evt_label ON audio_event(video_id, label);

CREATE TABLE IF NOT EXISTS candidate_clip (
    id          INTEGER PRIMARY KEY,
    video_id    INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    start_s     REAL NOT NULL,
    end_s       REAL NOT NULL,
    score       REAL NOT NULL,
    features    TEXT,
    transcript  TEXT,
    llm_rank    INTEGER,
    llm_title   TEXT,
    llm_desc    TEXT,
    llm_contents TEXT,
    llm_tags    TEXT,
    clip_path   TEXT,
    thumb_path  TEXT,
    user_rating INTEGER,
    active      INTEGER NOT NULL DEFAULT 1,
    title       TEXT,
    source      TEXT NOT NULL DEFAULT 'auto'
);
CREATE INDEX IF NOT EXISTS idx_clip_video ON candidate_clip(video_id, start_s);
CREATE INDEX IF NOT EXISTS idx_clip_score ON candidate_clip(video_id, score DESC);
CREATE INDEX IF NOT EXISTS idx_clip_active ON candidate_clip(video_id, active);
CREATE INDEX IF NOT EXISTS idx_clip_active_source ON candidate_clip(video_id, active, source);

CREATE TABLE IF NOT EXISTS vision_frame (
    id        INTEGER PRIMARY KEY,
    video_id  INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    ts_s      REAL    NOT NULL,
    sampled   TEXT    NOT NULL DEFAULT 'fixed',
    caption   TEXT,
    n_objs    INTEGER NOT NULL DEFAULT 0,
    n_ocr     INTEGER NOT NULL DEFAULT 0,
    raw_json  TEXT
);
CREATE INDEX IF NOT EXISTS idx_vf_video_ts ON vision_frame(video_id, ts_s);

CREATE TABLE IF NOT EXISTS vision_detection (
    id        INTEGER PRIMARY KEY,
    frame_id  INTEGER NOT NULL REFERENCES vision_frame(id) ON DELETE CASCADE,
    video_id  INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    ts_s      REAL    NOT NULL,
    label     TEXT    NOT NULL,
    conf      REAL    NOT NULL,
    bbox_x1   REAL, bbox_y1 REAL, bbox_x2 REAL, bbox_y2 REAL,
    area_frac REAL
);
CREATE INDEX IF NOT EXISTS idx_vd_video_ts    ON vision_detection(video_id, ts_s);
CREATE INDEX IF NOT EXISTS idx_vd_video_label ON vision_detection(video_id, label);

CREATE TABLE IF NOT EXISTS vision_ocr (
    id         INTEGER PRIMARY KEY,
    frame_id   INTEGER NOT NULL REFERENCES vision_frame(id) ON DELETE CASCADE,
    video_id   INTEGER NOT NULL REFERENCES video(id) ON DELETE CASCADE,
    ts_s       REAL    NOT NULL,
    text       TEXT    NOT NULL,
    text_upper TEXT    NOT NULL,
    conf       REAL,
    bbox_x1    REAL, bbox_y1 REAL, bbox_x2 REAL, bbox_y2 REAL,
    area_frac  REAL
);
CREATE INDEX IF NOT EXISTS idx_vo_video_ts ON vision_ocr(video_id, ts_s);
CREATE INDEX IF NOT EXISTS idx_vo_text     ON vision_ocr(text_upper);

CREATE TABLE IF NOT EXISTS job (
    id            INTEGER PRIMARY KEY,
    video_id      INTEGER REFERENCES video(id) ON DELETE CASCADE,
    library_id    INTEGER REFERENCES library(id) ON DELETE CASCADE,
    type          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    target_id     INTEGER,
    error_message TEXT,
    asynq_id      TEXT,
    progress      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT DEFAULT (datetime('now')),
    updated_at    TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_job_status  ON job(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_job_video   ON job(video_id, status);
CREATE INDEX IF NOT EXISTS idx_job_library ON job(library_id, status);
`

// PostMigrationIndexes covers columns added by migrations. Run after
// Migrations so legacy DBs (where e.g. job.asynq_id is added later) don't
// fail base-schema index creation.
const PostMigrationIndexes = `
CREATE INDEX IF NOT EXISTS idx_job_asynq ON job(asynq_id);
`

// Migrations are forward-only, idempotent. Matches Python _MIGRATIONS.
// Each entry: (table, column, DDL). Applied only if column missing.
var Migrations = []struct {
	Table  string
	Column string
	DDL    string
}{
	{"video", "game", "ALTER TABLE video ADD COLUMN game TEXT"},
	{"video", "recorded_at", "ALTER TABLE video ADD COLUMN recorded_at TEXT"},
	{"candidate_clip", "llm_contents", "ALTER TABLE candidate_clip ADD COLUMN llm_contents TEXT"},
	{"candidate_clip", "thumb_path", "ALTER TABLE candidate_clip ADD COLUMN thumb_path TEXT"},
	{"candidate_clip", "user_rating", "ALTER TABLE candidate_clip ADD COLUMN user_rating INTEGER"},
	{"candidate_clip", "active", "ALTER TABLE candidate_clip ADD COLUMN active INTEGER NOT NULL DEFAULT 1"},
	{"video", "vision_done", "ALTER TABLE video ADD COLUMN vision_done INTEGER NOT NULL DEFAULT 0"},
	{"candidate_clip", "title", "ALTER TABLE candidate_clip ADD COLUMN title TEXT"},
	{"candidate_clip", "source", "ALTER TABLE candidate_clip ADD COLUMN source TEXT NOT NULL DEFAULT 'auto'"},
	{"video", "library_id", "ALTER TABLE video ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE SET NULL"},
	{"video", "missing", "ALTER TABLE video ADD COLUMN missing INTEGER NOT NULL DEFAULT 0"},
	{"video", "sha1_full", "ALTER TABLE video ADD COLUMN sha1_full TEXT"},
	{"job", "library_id", "ALTER TABLE job ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE CASCADE"},
	{"job", "asynq_id", "ALTER TABLE job ADD COLUMN asynq_id TEXT"},
	{"job", "progress", "ALTER TABLE job ADD COLUMN progress INTEGER NOT NULL DEFAULT 0"},
	{"candidate_clip", "clip_stale", "ALTER TABLE candidate_clip ADD COLUMN clip_stale INTEGER NOT NULL DEFAULT 0"},
}
