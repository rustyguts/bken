"""Tests for the library management and sync feature.

These tests guarantee that:
  * Library sync ingests new files.
  * Missing files are marked missing in the DB, NEVER deleted from disk.
  * Moved files are detected via SHA1 and their DB rows are updated.
  * Removing a library marks videos missing but does NOT touch files.
"""

from __future__ import annotations

import os
import shutil
from pathlib import Path

import pytest

from bken import db as db_mod
from bken import ingest, library


@pytest.fixture
def tmp_db(tmp_path, monkeypatch):
    """Point the DB at a temporary path and initialise it."""
    monkeypatch.setattr(db_mod.config, "DB_PATH", tmp_path / "t.db")
    db_mod.init_db()
    # Monkeypatch ffprobe so dummy .mp4 files parse without invoking real ffmpeg.
    monkeypatch.setattr(ingest, "_ffprobe", lambda _path: {
        "format": {"duration": "120.5"},
        "streams": [
            {"codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080, "avg_frame_rate": "30/1"},
            {"codec_type": "audio", "codec_name": "aac", "channels": 2, "sample_rate": "48000"},
        ],
    })
    yield tmp_path / "t.db"


def _write_dummy_video(path: Path, content: bytes = b"dummy") -> None:
    """Create a small file that `ingest` will treat as a video."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content)


def test_db_init_creates_library_table(tmp_db):
    """After init_db the library table must exist."""
    import sqlite3
    conn = sqlite3.connect(tmp_db)
    tables = {r[0] for r in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    assert "library" in tables
    cols = {r[1] for r in conn.execute("PRAGMA table_info(video)")}
    assert "library_id" in cols
    assert "missing" in cols
    assert "sha1_full" in cols
    conn.close()


class TestLibrarySync:
    def test_sync_ingests_new_file(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"new file content")

        lid = library.add_library(lib_dir, name="Test")
        counts = library.sync_library(lid)

        assert counts["inserted"] == 1
        assert counts["missing"] == 0

        with db_mod.db() as conn:
            row = conn.execute("SELECT * FROM video WHERE library_id = ?", (lid,)).fetchone()
        assert row is not None
        assert row["missing"] == 0
        assert row["sha1_full"] is not None

    def test_sync_marks_removed_file_missing(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"content")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        # Remove file from disk
        vid.unlink()
        assert not vid.exists()

        counts = library.sync_library(lid)
        assert counts["missing"] == 1

        with db_mod.db() as conn:
            row = conn.execute("SELECT missing FROM video WHERE library_id = ?", (lid,)).fetchone()
        assert row["missing"] == 1

    def test_sync_never_deletes_files_on_disk(self, tmp_db, tmp_path):
        """CRITICAL: sync must NEVER unlink, rmdir, or otherwise mutate the filesystem."""
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"precious data")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        # After marking the file missing, the file must still exist.
        assert vid.exists(), "sync deleted a file from disk — this must NEVER happen"

        # Even after removing the library, the file must still exist.
        library.remove_library(lid)
        assert vid.exists(), "remove_library deleted a file from disk — this must NEVER happen"

    def test_sync_detects_move_via_sha1(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        old_path = lib_dir / "old_name.mp4"
        new_path = lib_dir / "sub" / "new_name.mp4"
        _write_dummy_video(old_path, b"move me")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        with db_mod.db() as conn:
            row = conn.execute("SELECT id, sha1_full FROM video WHERE path = ?", (str(old_path),)).fetchone()
        original_id = row["id"]
        original_sha1 = row["sha1_full"]

        # Move the file
        old_path.unlink()
        _write_dummy_video(new_path, b"move me")

        counts = library.sync_library(lid)
        assert counts["restored"] == 1, f"expected move detection (restored=1), got {counts}"
        assert counts["missing"] == 0

        with db_mod.db() as conn:
            moved = conn.execute("SELECT * FROM video WHERE id = ?", (original_id,)).fetchone()
            stale = conn.execute("SELECT * FROM video WHERE path = ?", (str(old_path),)).fetchone()
        assert moved is not None
        assert moved["path"] == str(new_path)
        assert moved["missing"] == 0
        assert moved["sha1_full"] == original_sha1
        assert stale is None, "old path row should have been reused, not left behind"

    def test_sync_restores_previously_missing_file(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"comes back")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        # Remove and sync → missing
        vid.unlink()
        library.sync_library(lid)

        # Put it back and sync → restored
        _write_dummy_video(vid, b"comes back")
        counts = library.sync_library(lid)
        assert counts["restored"] == 1

        with db_mod.db() as conn:
            row = conn.execute("SELECT missing FROM video WHERE library_id = ?", (lid,)).fetchone()
        assert row["missing"] == 0

    def test_sync_skips_unchanged_file(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"same")

        lid = library.add_library(lib_dir)
        counts1 = library.sync_library(lid)
        assert counts1["inserted"] == 1

        counts2 = library.sync_library(lid)
        assert counts2["unchanged"] == 1
        assert counts2["inserted"] == 0

    def test_remove_library_marks_missing_not_deletes(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"safe")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        library.remove_library(lid)

        with db_mod.db() as conn:
            row = conn.execute("SELECT missing FROM video WHERE library_id = ?", (lid,)).fetchone()
        assert row["missing"] == 1
        assert vid.exists(), "file was deleted from disk — NEVER acceptable"

    def test_delete_library_removes_rows_not_files(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"safe")

        lid = library.add_library(lib_dir)
        library.sync_library(lid)

        library.delete_library(lid)

        with db_mod.db() as conn:
            rows = conn.execute("SELECT * FROM video WHERE library_id = ?", (lid,)).fetchall()
            libs = conn.execute("SELECT * FROM library WHERE id = ?", (lid,)).fetchall()
        assert len(rows) == 0
        assert len(libs) == 0
        assert vid.exists(), "file was deleted from disk — NEVER acceptable"

    def test_dry_run_does_not_write(self, tmp_db, tmp_path):
        lib_dir = tmp_path / "archive"
        vid = lib_dir / "test.mp4"
        _write_dummy_video(vid, b"dry")

        lid = library.add_library(lib_dir)
        counts = library.sync_library(lid, dry_run=True)
        assert counts["inserted"] == 1

        with db_mod.db() as conn:
            rows = conn.execute("SELECT * FROM video WHERE library_id = ?", (lid,)).fetchall()
        assert len(rows) == 0, "dry_run should not have written any rows"


class TestSha1Full:
    def test_sha1_full_matches_sha1sum(self, tmp_path):
        """Our _sha1_full should match the system sha1sum when available."""
        f = tmp_path / "big.bin"
        f.write_bytes(b"x" * (1024 * 1024))  # 1 MB

        result = ingest._sha1_full(f)
        assert len(result) == 40  # SHA1 hex length

        if shutil.which("sha1sum"):
            import subprocess
            proc = subprocess.run(["sha1sum", str(f)], capture_output=True, text=True, check=True)
            expected = proc.stdout.split()[0]
            assert result == expected

    def test_sha1_full_on_empty_file(self, tmp_path):
        f = tmp_path / "empty.bin"
        f.write_bytes(b"")
        result = ingest._sha1_full(f)
        assert result == "da39a3ee5e6b4b0d3255bfef95601890afd80709"
