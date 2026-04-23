"""Smoke tests exercising the pure-Python bits of the pipeline.

Heavy ML stages (faster-whisper, PANNs) are not unit-tested here — they
need a GPU and large model downloads. Run the full pipeline against a
real video for end-to-end coverage.
"""

from __future__ import annotations

import sqlite3

import numpy as np

from bken import db as db_mod
from bken import density, scoring


def test_db_init(tmp_path, monkeypatch):
    monkeypatch.setattr(db_mod.config, "DB_PATH", tmp_path / "t.db")
    db_mod.init_db()
    conn = sqlite3.connect(tmp_path / "t.db")
    tables = {r[0] for r in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    assert {"video", "transcript_segment", "audio_event", "candidate_clip"} <= tables
    cols = {r[1] for r in conn.execute("PRAGMA table_info(candidate_clip)")}
    assert "active" in cols, "active column should be created by the migration"
    conn.close()


def test_scoring_keyword_match():
    w = scoring.Window(start=0, end=30, text="oh my god that was wild, dude")
    w.talk = 5
    w.kw = len(scoring.KEYWORD_RE.findall(w.text))
    w.spk = len(w.text.strip()) / w.talk
    assert w.kw >= 2  # "oh my god" + "dude"
    assert w.score() > 0


def test_scoring_empty_zero():
    w = scoring.Window(start=0, end=30)
    assert w.score() == 0.0


def test_non_max_suppression_keeps_top_and_separates():
    # Three overlapping windows should reduce to the best-scoring one
    a = scoring.Window(start=0, end=30)
    a.kw = 5  # forces a high score
    b = scoring.Window(start=10, end=40)
    b.kw = 3
    c = scoring.Window(start=100, end=130)
    c.kw = 1
    kept = scoring._non_max_suppress([a, b, c], min_gap=5, k=10)
    # a beats b (same region), c survives (far away)
    assert len(kept) == 2
    assert kept[0].start == 0
    assert kept[1].start == 100


# ──────────────────────────────────────────────────────────────────────────
# Density / hysteresis
# ──────────────────────────────────────────────────────────────────────────


def test_hysteresis_extends_through_dip():
    """A short sub-threshold dip inside an otherwise-hot region must not split it."""
    # 5s quiet, 3s loud, 2s quiet, 3s loud, 5s quiet
    I = np.array([0]*5 + [10]*3 + [0]*2 + [10]*3 + [0]*5, dtype=np.float32)
    regions = density.hysteresis_segments(
        I, high=5.0, low=1.0, gap_tol=4, min_len=1, max_len=100,
    )
    # gap_tol=4 should bridge the 2s dip and produce a single (5, 13) region.
    assert len(regions) == 1, f"expected 1 region, got {regions}"
    s, e = regions[0]
    assert s == 5 and e == 13


def test_hysteresis_respects_high_threshold():
    """A region above `low` but never reaching `high` should not be kept."""
    I = np.array([0, 0, 2, 2, 2, 2, 0, 0], dtype=np.float32)
    # low=1 includes the 2s; high=5 is never reached → no regions.
    assert density.hysteresis_segments(
        I, high=5.0, low=1.0, gap_tol=1, min_len=1, max_len=100,
    ) == []


def test_clamp_inflates_short_region():
    """Regions shorter than min_len should be padded out symmetrically."""
    I = np.zeros(50, dtype=np.float32)
    I[20:23] = 10  # 3-sample peak, well below min_len
    regions = density.hysteresis_segments(
        I, high=5.0, low=1.0, gap_tol=1, min_len=10, max_len=100,
    )
    assert len(regions) == 1
    s, e = regions[0]
    assert (e - s) >= 10
    assert s <= 20 and e >= 23  # original peak stays inside


def test_multi_scale_extends_peak_into_surrounding_context():
    """A loose threshold should sweep moderate signal around a peak into one long clip;
    a tight threshold over the same peak yields a short one. NMS picks the higher-scoring
    representation — typically the long one because score is sum(I[s:e])."""
    I = np.zeros(200, dtype=np.float32)
    I[80:120] = 5.0    # 40s of moderate sustained density
    I[95:105] = 15.0   # 10s crisp peak inside

    regions = density.multi_scale_segments(
        I,
        scales=[(95.0, 90.0), (85.0, 30.0)],  # tight + loose
        gap_tol=2, min_len=5, max_len=150, top_n=10,
    )
    assert len(regions) >= 1
    # The long region's score (sum 5s of plateau + 10s of peak) should beat the
    # tight peak alone, so NMS keeps it: expect a clip ≥ ~30s long.
    longest = max(int(r.end - r.start) for r in regions)
    assert longest >= 30, f"loose threshold should pull in surrounding context: {[(r.start, r.end) for r in regions]}"


def test_hysteresis_short_peak_alone():
    """If there's no surrounding context, the clip stays short — variable length, not always long."""
    I = np.zeros(100, dtype=np.float32)
    I[50:55] = 10.0  # 5s isolated peak
    regions = density.multi_scale_segments(
        I,
        scales=[(95.0, 85.0)],
        gap_tol=2, min_len=5, max_len=60, top_n=10,
    )
    assert len(regions) == 1
    assert (regions[0].end - regions[0].start) <= 12  # bounded near the peak width


# ──────────────────────────────────────────────────────────────────────────
# IoU-based stable identity (rescore safety)
# ──────────────────────────────────────────────────────────────────────────


def _row(id_, s, e, **kw):
    base = {"id": id_, "start_s": s, "end_s": e,
            "user_rating": None, "llm_title": None, "active": 1}
    base.update(kw)
    return base


def test_iou_matcher_preserves_id_for_overlapping_region():
    existing = [_row(1, 0, 30, user_rating=5), _row(2, 100, 130)]
    new = [
        density.Region(start=2, end=32, score=10.0, features={}),
        density.Region(start=200, end=230, score=8.0, features={}),
    ]
    matched, unmatched_new, unmatched_existing = density.match_existing(
        new, existing, iou_threshold=0.5,
    )
    matched_ids = {old["id"]: r for r, old in matched}
    # Rated row 1 should be reused for the (2,32) window — IoU ≈ 0.875.
    assert 1 in {old["id"] for _, old in matched}
    # The far-away (200,230) window can't match anything → unmatched_new.
    assert any(r.start == 200 for r in unmatched_new)
    # Row 2 has no match → unmatched_existing (will be deactivated).
    assert any(o["id"] == 2 for o in unmatched_existing)


def test_iou_matcher_tiebreak_prefers_rated():
    """When two existing rows tie on IoU, the rated one wins."""
    existing = [
        _row(1, 0, 30),                  # unrated
        _row(2, 0, 30, user_rating=4),   # rated — should win the tie
    ]
    new = [density.Region(start=0, end=30, score=10.0, features={})]
    matched, _, _ = density.match_existing(new, existing, iou_threshold=0.5)
    assert len(matched) == 1
    assert matched[0][1]["id"] == 2


def test_iou_matcher_skips_below_threshold():
    """Two windows that barely touch (IoU < 0.5) must NOT be matched."""
    existing = [_row(1, 0, 30)]
    # New window at (25, 55): overlap=5, union=55 → IoU ≈ 0.09
    new = [density.Region(start=25, end=55, score=10.0, features={})]
    matched, unmatched_new, unmatched_existing = density.match_existing(
        new, existing, iou_threshold=0.5,
    )
    assert matched == []
    assert len(unmatched_new) == 1
    assert len(unmatched_existing) == 1
