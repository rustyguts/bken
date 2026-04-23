"""Audio-event detection: laughter, shouts, cheers, RMS energy.

Pipeline:
  1. Decode audio once at 32 kHz (PANNs' native rate).
  2. Slide a 3-second window with 1-second stride over the audio and feed
     batches to a pretrained PANNs `SoundEventDetection` (Cnn14) model.
     For each window we max-pool the per-frame probabilities of a small
     group of AudioSet labels ("Laughter" group, "Shout" group, "Cheer"
     group) and record the window's peak per-group probability.
  3. Run-length-encode windows whose probability exceeds a low absolute
     threshold into `audio_event` rows. Thresholds are *relative* — real
     laughter in gaming recordings peaks at ~0.1 because music/engine
     noise competes, so we can't use the 0.3 cutoff you'd use for clean
     podcast audio.
  4. Separately, emit "LoudSpike" events where short-term RMS energy is
     more than 2 σ above the file's median — catches explosions,
     gunfire bursts, and overlapping shouts that PANNs may miss.

All heavy-lifting happens on GPU in batched torch.no_grad() forwards, so
a 30-minute file processes in a few seconds on a consumer GPU.
"""

from __future__ import annotations

import logging
from pathlib import Path

import librosa
import numpy as np
import torch
from rich.console import Console
from rich.progress import Progress, TimeElapsedColumn

from . import audio as audio_mod
from . import config, db

log = logging.getLogger(__name__)
console = Console()

PANNS_SR = 32000
WINDOW_S = 3.0
STRIDE_S = 1.0
BATCH_SIZE = 32

# AudioSet labels we care about, grouped under higher-level categories.
EVENT_GROUPS: dict[str, list[str]] = {
    "Laughter": ["Laughter", "Giggle", "Belly laugh", "Baby laughter"],
    "Shout":    ["Shout", "Yell", "Screaming", "Children shouting"],
    "Cheer":    ["Cheering", "Crowd", "Clapping"],
    # Gunfire is split out so scoring can explicitly *downweight* it —
    # in a shooter, "someone is firing a gun" is the default state, not
    # a signal of something funny.
    "Gunfire":  ["Gunshot, gunfire", "Machine gun", "Fusillade", "Cap gun", "Artillery fire"],
}

# Calibrated against gaming recordings where game audio competes with
# voice. For reference, Laughter peaks at ~0.13 in a loud GTA session
# and ~0.6 in a clean podcast.
EVENT_THRESHOLDS: dict[str, float] = {
    "Laughter": 0.05,
    "Shout":    0.03,
    "Cheer":    0.05,
    "Gunfire":  0.10,
}

MIN_EVENT_DURATION = 1.0
MAX_GAP_MERGE = 1.5

_sed = None


def _get_sed():
    global _sed
    if _sed is None:
        from panns_inference import SoundEventDetection
        console.print("[cyan]Loading PANNs sound-event detector…[/]")
        _sed = SoundEventDetection(device="cuda")
    return _sed


def _label_index() -> dict[str, int]:
    from panns_inference import labels
    return {lbl: i for i, lbl in enumerate(labels)}


def _merge_close(events: list[tuple[float, float, float]]) -> list[tuple[float, float, float]]:
    if not events:
        return events
    events = sorted(events)
    merged = [events[0]]
    for s, e, p in events[1:]:
        ps, pe, pp = merged[-1]
        if s - pe <= MAX_GAP_MERGE:
            merged[-1] = (ps, max(pe, e), max(pp, p))
        else:
            merged.append((s, e, p))
    return merged


def _windows_to_events(
    window_probs: np.ndarray,
    window_starts: np.ndarray,
    window_len: float,
    threshold: float,
) -> list[tuple[float, float, float]]:
    """Turn a 1-D time series of per-window peak probs into merged events.

    A window "fires" when its peak prob > threshold. Firing windows extend
    from their start to start+window_len, and overlapping/adjacent firings
    are merged.
    """
    fires = window_probs > threshold
    if not fires.any():
        return []
    events = []
    for i, firing in enumerate(fires):
        if firing:
            events.append((float(window_starts[i]), float(window_starts[i] + window_len), float(window_probs[i])))
    return _merge_close([e for e in events if (e[1] - e[0]) >= MIN_EVENT_DURATION] or events)


def detect_events_for_video(video_id: int, audio_file: Path) -> dict[str, int]:
    """Run PANNs SED + RMS on one audio file. Returns per-label event counts."""
    sed = _get_sed()
    y, _sr = librosa.load(str(audio_file), sr=PANNS_SR, mono=True)
    total_s = len(y) / PANNS_SR

    lbl_index = _label_index()
    group_idxs = {g: [lbl_index[l] for l in ls if l in lbl_index]
                  for g, ls in EVENT_GROUPS.items()}

    window_samples = int(WINDOW_S * PANNS_SR)
    stride_samples = int(STRIDE_S * PANNS_SR)
    # Build window start-sample positions
    starts = np.arange(0, max(1, len(y) - window_samples + 1), stride_samples)

    group_peaks = {g: np.zeros(len(starts), dtype=np.float32) for g in EVENT_GROUPS}

    with Progress(
        *Progress.get_default_columns(), TimeElapsedColumn(), console=console
    ) as prog:
        task = prog.add_task(f"PANNs SED video_id={video_id}", total=len(starts))
        for batch_start in range(0, len(starts), BATCH_SIZE):
            batch_idxs = starts[batch_start : batch_start + BATCH_SIZE]
            # Build (B, window_samples) tensor
            batch = np.stack(
                [y[s : s + window_samples] for s in batch_idxs], axis=0
            )
            with torch.no_grad():
                # sed.inference returns (B, frames, 527)
                out = sed.inference(batch)
            # Max over frames → per-window per-label peak
            peaks = out.max(axis=1)  # (B, 527)
            for g, idxs in group_idxs.items():
                group_peaks[g][batch_start : batch_start + len(batch_idxs)] = peaks[:, idxs].max(axis=1)
            prog.update(task, advance=len(batch_idxs))

    starts_s = starts / PANNS_SR
    all_events: dict[str, list[tuple[float, float, float]]] = {}
    for g in EVENT_GROUPS:
        all_events[g] = _windows_to_events(
            group_peaks[g], starts_s, WINDOW_S, EVENT_THRESHOLDS[g]
        )

    rms_events = _rms_spike_events(y, sr=PANNS_SR)

    counts = {g: len(evts) for g, evts in all_events.items()}
    counts["LoudSpike"] = len(rms_events)

    with db.db() as conn:
        conn.execute("DELETE FROM audio_event WHERE video_id = ?", (video_id,))
        for group, evts in all_events.items():
            for s, e, p in evts:
                conn.execute(
                    "INSERT INTO audio_event (video_id, start_s, end_s, label, score) VALUES (?,?,?,?,?)",
                    (video_id, float(s), float(e), group, float(p)),
                )
        for s, e, p in rms_events:
            conn.execute(
                "INSERT INTO audio_event (video_id, start_s, end_s, label, score) VALUES (?,?,?,?,?)",
                (video_id, float(s), float(e), "LoudSpike", float(p)),
            )
        conn.execute(
            "UPDATE video SET events_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
    return counts


def _rms_spike_events(y: np.ndarray, sr: int) -> list[tuple[float, float, float]]:
    hop = int(sr * 0.1)    # 100 ms
    frame = int(sr * 0.4)  # 400 ms window
    rms = librosa.feature.rms(y=y, frame_length=frame, hop_length=hop)[0]
    db_vals = librosa.amplitude_to_db(rms + 1e-8)
    med = np.median(db_vals)
    std = np.std(db_vals)
    threshold = med + 2 * std
    above = db_vals > threshold

    events: list[tuple[float, float, float]] = []
    i = 0
    n = len(above)
    while i < n:
        if not above[i]:
            i += 1
            continue
        j = i
        while j < n and above[j]:
            j += 1
        start = i * hop / sr
        end = j * hop / sr
        peak = float((db_vals[i:j].max() - med) / (std + 1e-6))
        if (end - start) >= 0.3:
            events.append((start, end, peak))
        i = j
    return _merge_close(events)


def detect_pending(force: bool = False) -> dict[int, dict[str, int]]:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id FROM video
               WHERE audio_done = 1 AND (events_done = 0 OR ? = 1)""",
            (1 if force else 0,),
        ).fetchall()
    results = {}
    for row in rows:
        audio_file = audio_mod.audio_path_for(row["id"])
        if not audio_file.exists():
            log.warning("missing audio for video_id=%s", row["id"])
            continue
        results[row["id"]] = detect_events_for_video(row["id"], audio_file)
    return results
