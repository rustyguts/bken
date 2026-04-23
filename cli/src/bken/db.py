"""SQLite schema + helpers.

Single-file DB is plenty for a single-user archive. Each pipeline stage
reads and writes a small number of tables; stage state lives on the
`video` row so the pipeline is resumable and idempotent.
"""

from __future__ import annotations

import sqlite3
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator

from . import config

SCHEMA = """
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
    sha1_prefix     TEXT,            -- fast prefix hash for change detection
    sha1_full       TEXT,            -- full file hash for move detection
    duration_s      REAL,
    width           INTEGER,
    height          INTEGER,
    fps             REAL,
    video_codec     TEXT,
    audio_codec     TEXT,
    audio_channels  INTEGER,
    audio_rate      INTEGER,
    -- UI-facing derived fields
    game            TEXT,            -- parent dir name (e.g. "Grand_Theft_Auto_V")
    recorded_at     TEXT,            -- ISO-ish timestamp parsed from filename; falls back to mtime
    -- library linkage + presence tracking
    library_id      INTEGER REFERENCES library(id) ON DELETE SET NULL,
    missing         INTEGER NOT NULL DEFAULT 0,
    -- stage completion flags
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
    label       TEXT NOT NULL,     -- e.g., "Laughter", "Shout", "Cheering"
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
    features    TEXT,              -- JSON breakdown of score components
    transcript  TEXT,              -- concatenated transcript text for this window
    -- LLM rank output (filled after rank stage)
    llm_rank    INTEGER,
    llm_title   TEXT,
    llm_desc    TEXT,              -- one-sentence "reason for ranking"
    llm_contents TEXT,             -- 2-3 sentence narrative of what happens
    llm_tags    TEXT,
    clip_path   TEXT,              -- set after ffmpeg cut
    thumb_path  TEXT,              -- set on first thumbnail render
    user_rating INTEGER,           -- 0-5 stars from the UI, NULL = unrated
    active      INTEGER NOT NULL DEFAULT 1,
    -- user-editable metadata
    title       TEXT,              -- user-editable display name; falls back to llm_title
    source      TEXT NOT NULL DEFAULT 'auto'  -- 'auto' | 'manual'
);
CREATE INDEX IF NOT EXISTS idx_clip_video ON candidate_clip(video_id, start_s);
CREATE INDEX IF NOT EXISTS idx_clip_score ON candidate_clip(video_id, score DESC);
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

-- Job queue for pipeline stages, clip creation, and library sync.
CREATE TABLE IF NOT EXISTS job (
    id          INTEGER PRIMARY KEY,
    video_id    INTEGER REFERENCES video(id) ON DELETE CASCADE,
    library_id  INTEGER REFERENCES library(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,            -- ingest, extract_audio, transcribe, detect_events, detect_vision, score, rank, create_clip, reprocess, library_sync
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending | running | done | failed
    target_id   INTEGER,                  -- candidate_clip id for create_clip
    error_message TEXT,
    created_at  TEXT DEFAULT (datetime('now')),
    updated_at  TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_job_status ON job(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_job_video  ON job(video_id, status);
CREATE INDEX IF NOT EXISTS idx_job_library ON job(library_id, status);
"""


def connect(db_path: Path | None = None) -> sqlite3.Connection:
    path = db_path or config.DB_PATH
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path, isolation_level=None, timeout=30.0)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL;")
    conn.execute("PRAGMA foreign_keys=ON;")
    return conn


def init_db(db_path: Path | None = None) -> None:
    conn = connect(db_path)
    try:
        conn.executescript(SCHEMA)
        _migrate(conn)
    finally:
        conn.close()


# Cheap, forward-only migrations for DBs created by older builds.
_MIGRATIONS: list[tuple[str, str, str]] = [
    ("video",          "game",         "ALTER TABLE video ADD COLUMN game TEXT"),
    ("video",          "recorded_at",  "ALTER TABLE video ADD COLUMN recorded_at TEXT"),
    ("candidate_clip", "llm_contents", "ALTER TABLE candidate_clip ADD COLUMN llm_contents TEXT"),
    ("candidate_clip", "thumb_path",   "ALTER TABLE candidate_clip ADD COLUMN thumb_path TEXT"),
    ("candidate_clip", "user_rating",  "ALTER TABLE candidate_clip ADD COLUMN user_rating INTEGER"),
    ("candidate_clip", "active",       "ALTER TABLE candidate_clip ADD COLUMN active INTEGER NOT NULL DEFAULT 1"),
    ("video",          "vision_done",  "ALTER TABLE video ADD COLUMN vision_done INTEGER NOT NULL DEFAULT 0"),
    ("candidate_clip", "title",        "ALTER TABLE candidate_clip ADD COLUMN title TEXT"),
    ("candidate_clip", "source",       "ALTER TABLE candidate_clip ADD COLUMN source TEXT NOT NULL DEFAULT 'auto'"),
    ("job",            "type",         "ALTER TABLE job ADD COLUMN type TEXT"),  # no-op if table already has it
    ("video",          "library_id",   "ALTER TABLE video ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE SET NULL"),
    ("video",          "missing",      "ALTER TABLE video ADD COLUMN missing INTEGER NOT NULL DEFAULT 0"),
    ("video",          "sha1_full",    "ALTER TABLE video ADD COLUMN sha1_full TEXT"),
    ("job",            "library_id",   "ALTER TABLE job ADD COLUMN library_id INTEGER REFERENCES library(id) ON DELETE CASCADE"),
]


def _migrate(conn: sqlite3.Connection) -> None:
    # Create library table on older DBs that don't have it yet.
    tables = {r["name"] for r in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    if "library" not in tables:
        conn.execute("""
            CREATE TABLE library (
                id INTEGER PRIMARY KEY,
                name TEXT NOT NULL,
                path TEXT NOT NULL UNIQUE,
                scan_interval_minutes INTEGER NOT NULL DEFAULT 60,
                last_scan_at TEXT,
                next_scan_at TEXT,
                active INTEGER NOT NULL DEFAULT 1,
                created_at TEXT DEFAULT (datetime('now'))
            )
        """)
        conn.execute("CREATE INDEX idx_library_path ON library(path)")
        conn.execute("CREATE INDEX idx_library_active ON library(active)")

    for table, column, ddl in _MIGRATIONS:
        cols = {r["name"] for r in conn.execute(f"PRAGMA table_info({table})")}
        if column not in cols:
            conn.execute(ddl)
    conn.execute("CREATE INDEX IF NOT EXISTS idx_clip_active ON candidate_clip(video_id, active)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_clip_active_source ON candidate_clip(video_id, active, source)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_job_status ON job(status, updated_at)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_job_video ON job(video_id, status)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_job_library ON job(library_id, status)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_video_library ON video(library_id)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_video_missing ON video(missing)")


@contextmanager
def db() -> Iterator[sqlite3.Connection]:
    conn = connect()
    try:
        yield conn
    finally:
        conn.close()


def touch(conn: sqlite3.Connection, video_id: int) -> None:
    conn.execute(
        "UPDATE video SET updated_at = datetime('now') WHERE id = ?", (video_id,)
    )
