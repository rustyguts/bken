"""CPU-only unit tests for the vision pipeline.

Heavy Florence-2 forwards are skipped here — they need a GPU and large model
downloads. Run the full pipeline against a real video for end-to-end coverage.
"""

from __future__ import annotations

import sqlite3

import numpy as np

from bken import config, db as db_mod, density


def test_vision_schema_migration(tmp_path, monkeypatch):
    monkeypatch.setattr(db_mod.config, "DB_PATH", tmp_path / "t.db")
    db_mod.init_db()
    conn = sqlite3.connect(tmp_path / "t.db")
    tables = {r[0] for r in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    assert {"vision_frame", "vision_detection", "vision_ocr"} <= tables
    cols = {r[1] for r in conn.execute("PRAGMA table_info(video)")}
    assert "vision_done" in cols, "vision_done column should be created by migration"
    conn.close()


def test_sample_schedule_builder():
    from bken.vision import _sample_schedule

    # Patch config for deterministic test
    original_fps = config.VISION_FPS
    original_scene = config.VISION_SCENE_DETECT
    original_max = config.VISION_MAX_FRAMES
    try:
        config.VISION_FPS = 1.0
        config.VISION_SCENE_DETECT = False
        config.VISION_MAX_FRAMES = 5000
        # We can't open a real video here, so test the pure math path
        # (scene detection disabled → just grid)
        # _sample_schedule needs a real Path when scene detect is on;
        # with it off we can pass a dummy path.
        schedule = _sample_schedule(__file__, duration_s=120.0)
        assert schedule[0] == 0.0
        assert schedule[-1] >= 119.0
        assert len(schedule) == 121  # 0..120 inclusive
    finally:
        config.VISION_FPS = original_fps
        config.VISION_SCENE_DETECT = original_scene
        config.VISION_MAX_FRAMES = original_max


def test_sample_schedule_with_scenes():
    from bken.vision import _sample_schedule

    original_fps = config.VISION_FPS
    original_scene = config.VISION_SCENE_DETECT
    original_max = config.VISION_MAX_FRAMES
    try:
        config.VISION_FPS = 1.0
        config.VISION_SCENE_DETECT = True
        config.VISION_MAX_FRAMES = 5000
        # scene detection will fail on __file__ (not a video), but the
        # function catches the exception and falls back to grid only.
        schedule = _sample_schedule(__file__, duration_s=120.0)
        assert len(schedule) == 121
    finally:
        config.VISION_FPS = original_fps
        config.VISION_SCENE_DETECT = original_scene
        config.VISION_MAX_FRAMES = original_max


def test_density_no_regression():
    """When all vision_* lists are empty, build_density returns identical
    output to the pre-vision code path."""
    duration = 60.0
    I = density.build_density(
        duration_s=duration,
        audio_events=[{"start_s": 10, "end_s": 15, "label": "Laughter", "score": 0.5}],
        transcript_segments=[{"start_s": 12, "end_s": 14, "text": "oh my god dude"}],
        sigma=0.0,
        vision_frames=[],
        vision_ocr=[],
        vision_detections=[],
        vision_enabled=True,
    )
    I_old = density.build_density(
        duration_s=duration,
        audio_events=[{"start_s": 10, "end_s": 15, "label": "Laughter", "score": 0.5}],
        transcript_segments=[{"start_s": 12, "end_s": 14, "text": "oh my god dude"}],
        sigma=0.0,
    )
    assert np.allclose(I, I_old)


def test_density_ocr_keyword():
    """Synthetic OCR row at t=100 with 'WASTED' and large area → peak."""
    duration = 120.0
    I = density.build_density(
        duration_s=duration,
        audio_events=[],
        transcript_segments=[],
        sigma=0.0,
        vision_frames=[],
        vision_ocr=[{"ts_s": 100.0, "text": "WASTED", "area_frac": 0.3}],
        vision_detections=[],
        vision_enabled=True,
    )
    expected = (
        density.WEIGHTS["vis_ocr_generic"]
        + density.WEIGHTS["vis_ocr_keyword"]
        * (1.0 + density.WEIGHTS["vis_ocr_large"])
    )
    assert np.isclose(I[100], expected), f"expected {expected}, got {I[100]}"
    assert np.all(I[:100] == 0.0)
    assert np.all(I[101:] == 0.0)


def test_reset_vision(tmp_path, monkeypatch):
    monkeypatch.setattr(db_mod.config, "DB_PATH", tmp_path / "t.db")
    db_mod.init_db()
    conn = sqlite3.connect(tmp_path / "t.db")
    conn.execute("PRAGMA foreign_keys=ON")
    conn.execute(
        "INSERT INTO video (path, size_bytes, mtime, vision_done, score_done) VALUES (?, ?, ?, 1, 1)",
        ("/fake.mp4", 100, 1.0),
    )
    vid = conn.execute("SELECT id FROM video").fetchone()[0]
    conn.execute(
        "INSERT INTO vision_frame (video_id, ts_s, sampled) VALUES (?, ?, ?)",
        (vid, 5.0, "fixed"),
    )
    fid = conn.execute("SELECT id FROM vision_frame").fetchone()[0]
    conn.execute(
        "INSERT INTO vision_detection (frame_id, video_id, ts_s, label, conf) VALUES (?, ?, ?, ?, ?)",
        (fid, vid, 5.0, "person", 0.9),
    )
    conn.execute(
        "INSERT INTO vision_ocr (frame_id, video_id, ts_s, text, text_upper, conf) VALUES (?, ?, ?, ?, ?, ?)",
        (fid, vid, 5.0, "hello", "HELLO", 0.9),
    )
    conn.commit()
    conn.close()

    # Use the module reset_vision via a fresh connection
    from bken.vision import reset_vision

    conn = sqlite3.connect(tmp_path / "t.db")
    conn.execute("PRAGMA foreign_keys=ON")
    reset_vision(conn, vid)
    conn.commit()
    conn.close()

    conn = sqlite3.connect(tmp_path / "t.db")
    conn.row_factory = sqlite3.Row
    n_vf = conn.execute("SELECT COUNT(*) c FROM vision_frame WHERE video_id=?", (vid,)).fetchone()["c"]
    n_vd = conn.execute("SELECT COUNT(*) c FROM vision_detection WHERE video_id=?", (vid,)).fetchone()["c"]
    n_vo = conn.execute("SELECT COUNT(*) c FROM vision_ocr WHERE video_id=?", (vid,)).fetchone()["c"]
    row = conn.execute("SELECT vision_done, score_done FROM video WHERE id=?", (vid,)).fetchone()
    conn.close()
    assert n_vf == 0
    assert n_vd == 0
    assert n_vo == 0
    assert row is not None
    assert row["vision_done"] == 0
    assert row["score_done"] == 0
