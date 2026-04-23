"""Library management and periodic sync.

A library is a user-created mount point (directory). Videos inside it are
automatically ingested on scan. If a file disappears from disk it is marked
``missing`` in the DB — the file on disk is NEVER deleted. If a file is moved
within the library, its full SHA1 hash lets us detect the move and update the
path without losing transcripts, clips, or ratings.
"""

from __future__ import annotations

import logging
import sqlite3
from pathlib import Path
from typing import Optional

from . import config, db, ingest

log = logging.getLogger(__name__)

STATUS_UNCHANGED = "unchanged"
STATUS_UPDATED = "updated"
STATUS_INSERTED = "inserted"
STATUS_RESTORED = "restored"
STATUS_MISSING = "missing"
STATUS_ERRORED = "errored"
ALL_STATUSES = (STATUS_UNCHANGED, STATUS_UPDATED, STATUS_INSERTED,
                STATUS_RESTORED, STATUS_MISSING, STATUS_ERRORED)


# ──────────────────────────────────────────────────────────────────────────
# CRUD
# ──────────────────────────────────────────────────────────────────────────


def add_library(path: Path, name: Optional[str] = None,
                scan_interval_minutes: int = 60) -> int:
    """Add a new library and return its id."""
    db.init_db()
    resolved = str(path.resolve())
    with db.db() as conn:
        existing = conn.execute(
            "SELECT id FROM library WHERE path = ?", (resolved,)
        ).fetchone()
        if existing:
            return existing["id"]
        cur = conn.execute(
            "INSERT INTO library (name, path, scan_interval_minutes) VALUES (?, ?, ?)",
            (name or path.name, resolved, scan_interval_minutes),
        )
        return cur.lastrowid  # type: ignore[return-value]


def list_libraries() -> list[dict]:
    with db.db() as conn:
        rows = conn.execute("""
            SELECT l.*,
                   COUNT(v.id) AS video_count,
                   SUM(CASE WHEN v.missing = 1 THEN 1 ELSE 0 END) AS missing_count
            FROM library l
            LEFT JOIN video v ON v.library_id = l.id
            GROUP BY l.id
            ORDER BY l.name
        """).fetchall()
    return [dict(r) for r in rows]


def get_library(library_id: int) -> Optional[dict]:
    with db.db() as conn:
        row = conn.execute("SELECT * FROM library WHERE id = ?", (library_id,)).fetchone()
    return dict(row) if row else None


def remove_library(library_id: int) -> None:
    """Deactivate a library and mark all its videos as missing.

    Files on disk are NEVER touched.
    """
    with db.db() as conn:
        conn.execute("UPDATE library SET active = 0 WHERE id = ?", (library_id,))
        conn.execute(
            "UPDATE video SET missing = 1, updated_at = datetime('now') WHERE library_id = ?",
            (library_id,),
        )


def delete_library(library_id: int) -> None:
    """Hard-delete a library row and its video rows (cascades).

    Still NEVER touches files on disk.
    """
    with db.db() as conn:
        conn.execute("DELETE FROM library WHERE id = ?", (library_id,))


# ──────────────────────────────────────────────────────────────────────────
# Sync
# ──────────────────────────────────────────────────────────────────────────


def sync_library(library_id: int, dry_run: bool = False) -> dict[str, int]:
    """Scan a library's directory and reconcile with the DB.

    Returns counts with keys: unchanged, updated, inserted, restored,
    missing, errored.
    """
    lib = get_library(library_id)
    if not lib:
        raise ValueError(f"Library {library_id} not found")

    lib_path = Path(lib["path"])
    if not lib_path.exists():
        raise ValueError(f"Library path does not exist: {lib_path}")

    counts = {s: 0 for s in ALL_STATUSES}

    # Resolve every video file currently on disk.
    disk_paths: dict[Path, Path] = {}
    for p in ingest.iter_videos(lib_path):
        disk_paths[p.resolve()] = p
    disk_set = set(disk_paths.keys())

    # Load all DB rows for this library.
    with db.db() as conn:
        db_rows = conn.execute(
            "SELECT id, path, size_bytes, mtime, sha1_full, missing FROM video WHERE library_id = ?",
            (library_id,),
        ).fetchall()

    db_by_path: dict[Path, sqlite3.Row] = {}
    db_by_id: dict[int, sqlite3.Row] = {}
    for r in db_rows:
        rp = Path(r["path"]).resolve()
        db_by_path[rp] = r
        db_by_id[r["id"]] = r

    db_set = set(db_by_path.keys())
    touched_ids: set[int] = set()

    # 1. Files present on disk AND in DB.
    for path in disk_set & db_set:
        row = db_by_path[path]
        stat = path.stat()
        unchanged = row["size_bytes"] == stat.st_size and abs(row["mtime"] - stat.st_mtime) < 1
        if unchanged:
            if row["missing"]:
                if not dry_run:
                    with db.db() as conn:
                        conn.execute(
                            "UPDATE video SET missing = 0, updated_at = datetime('now') WHERE id = ?",
                            (row["id"],),
                        )
                counts[STATUS_RESTORED] += 1
            else:
                counts[STATUS_UNCHANGED] += 1
            touched_ids.add(row["id"])
        else:
            # File changed — re-ingest, preserving the row id.
            if not dry_run:
                status = _reingest(path, row["id"], library_id)
            else:
                status = STATUS_UPDATED
            counts[status] += 1
            touched_ids.add(row["id"])

    # 2. DB rows not on disk — mark missing FIRST so move detection works.
    newly_missing_ids: set[int] = set()
    for path in db_set - disk_set:
        row = db_by_path[path]
        if not row["missing"]:
            if not dry_run:
                with db.db() as conn:
                    conn.execute(
                        "UPDATE video SET missing = 1, updated_at = datetime('now') WHERE id = ?",
                        (row["id"],),
                    )
            counts[STATUS_MISSING] += 1
            touched_ids.add(row["id"])
            newly_missing_ids.add(row["id"])

    # 3. Files on disk but NOT in DB — new or moved.
    for path in disk_set - db_set:
        # Check for a move: look for a video in this library with the same sha1_full.
        # We check among rows that were just marked missing OR already missing.
        moved_to = None
        if not dry_run:
            sha1 = ingest._sha1_full(path)
            with db.db() as conn:
                moved_row = conn.execute(
                    "SELECT id FROM video WHERE library_id = ? AND sha1_full = ? ORDER BY missing DESC, updated_at DESC LIMIT 1",
                    (library_id, sha1),
                ).fetchone()
                if moved_row:
                    moved_to = moved_row["id"]

        if moved_to is not None and not dry_run:
            # It's a move! Update path and mark present.
            _update_path(path, moved_to, library_id)
            counts[STATUS_RESTORED] += 1
            touched_ids.add(moved_to)
            # If we counted this as missing in step 2, undo that count.
            if moved_to in newly_missing_ids:
                counts[STATUS_MISSING] -= 1
        else:
            if not dry_run:
                status = _ingest_new(path, library_id)
            else:
                status = STATUS_INSERTED
            counts[status] += 1

    # Update library scan timestamp.
    if not dry_run:
        with db.db() as conn:
            conn.execute(
                "UPDATE library SET last_scan_at = datetime('now') WHERE id = ?",
                (library_id,),
            )

    return counts


# ──────────────────────────────────────────────────────────────────────────
# Low-level helpers
# ──────────────────────────────────────────────────────────────────────────


def _reingest(path: Path, video_id: int, library_id: int) -> str:
    """Re-ingest a changed file, keeping the same row id."""
    try:
        stat = path.stat()
        probe = ingest._ffprobe(path)
        fields = ingest._extract_fields(probe)
        sha1p = ingest._sha1_prefix(path)
        sha1f = ingest._sha1_full(path)
        game = ingest._game_from_path(path)
        recorded_at = ingest._parse_recorded_at(path, stat.st_mtime)

        with db.db() as conn:
            conn.execute(
                """UPDATE video SET
                    path=?, size_bytes=?, mtime=?, sha1_prefix=?, sha1_full=?,
                    duration_s=?, width=?, height=?, fps=?,
                    video_codec=?, audio_codec=?, audio_channels=?, audio_rate=?,
                    game=?, recorded_at=?, library_id=?, missing=0,
                    audio_done=0, transcribe_done=0, events_done=0, score_done=0, rank_done=0,
                    updated_at=datetime('now')
                   WHERE id=?""",
                (
                    str(path), stat.st_size, stat.st_mtime, sha1p, sha1f,
                    fields["duration_s"], fields["width"], fields["height"], fields["fps"],
                    fields["video_codec"], fields["audio_codec"],
                    fields["audio_channels"], fields["audio_rate"],
                    game, recorded_at, library_id,
                    video_id,
                ),
            )
        return STATUS_UPDATED
    except Exception as e:
        log.warning("Re-ingest failed on %s: %s", path, e)
        return STATUS_ERRORED


def _ingest_new(path: Path, library_id: int) -> str:
    """Ingest a brand-new file."""
    try:
        stat = path.stat()
        probe = ingest._ffprobe(path)
        fields = ingest._extract_fields(probe)
        sha1p = ingest._sha1_prefix(path)
        sha1f = ingest._sha1_full(path)
        game = ingest._game_from_path(path)
        recorded_at = ingest._parse_recorded_at(path, stat.st_mtime)

        with db.db() as conn:
            conn.execute(
                """INSERT INTO video (
                    path, size_bytes, mtime, sha1_prefix, sha1_full, library_id,
                    duration_s, width, height, fps,
                    video_codec, audio_codec, audio_channels, audio_rate,
                    game, recorded_at
                   ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
                (
                    str(path), stat.st_size, stat.st_mtime, sha1p, sha1f, library_id,
                    fields["duration_s"], fields["width"], fields["height"], fields["fps"],
                    fields["video_codec"], fields["audio_codec"],
                    fields["audio_channels"], fields["audio_rate"],
                    game, recorded_at,
                ),
            )
        return STATUS_INSERTED
    except Exception as e:
        log.warning("Ingest failed on %s: %s", path, e)
        return STATUS_ERRORED


def _update_path(path: Path, video_id: int, library_id: int) -> None:
    """Update the path of a moved file and mark it present."""
    stat = path.stat()
    probe = ingest._ffprobe(path)
    fields = ingest._extract_fields(probe)
    sha1p = ingest._sha1_prefix(path)
    sha1f = ingest._sha1_full(path)
    game = ingest._game_from_path(path)
    recorded_at = ingest._parse_recorded_at(path, stat.st_mtime)

    with db.db() as conn:
        conn.execute(
            """UPDATE video SET
                path=?, size_bytes=?, mtime=?, sha1_prefix=?, sha1_full=?,
                duration_s=?, width=?, height=?, fps=?,
                video_codec=?, audio_codec=?, audio_channels=?, audio_rate=?,
                game=?, recorded_at=?, library_id=?, missing=0,
                updated_at=datetime('now')
               WHERE id=?""",
            (
                str(path), stat.st_size, stat.st_mtime, sha1p, sha1f,
                fields["duration_s"], fields["width"], fields["height"], fields["fps"],
                fields["video_codec"], fields["audio_codec"],
                fields["audio_channels"], fields["audio_rate"],
                game, recorded_at, library_id,
                video_id,
            ),
        )
