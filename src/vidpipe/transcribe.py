"""Transcribe extracted audio with faster-whisper on the GPU.

We use `large-v3` with VAD filtering by default — gaming recordings have
long music-only / silent stretches, and VAD shaves a lot of wasted
model time. Segment-level timestamps are enough for clip boundaries; we
snap to silence separately in the scoring stage.

Model and compute type are overridable via env (`VIDPIPE_WHISPER_MODEL`,
`VIDPIPE_WHISPER_COMPUTE`) so you can fall back to `turbo` or int8 on
tighter hardware.
"""

from __future__ import annotations

import logging
from pathlib import Path

from rich.console import Console
from rich.progress import Progress, TimeElapsedColumn, BarColumn, TextColumn

from . import audio as audio_mod
from . import config, db

log = logging.getLogger(__name__)
console = Console()

_model = None  # lazy — loading large-v3 takes several seconds


def _get_model():
    global _model
    if _model is None:
        from faster_whisper import WhisperModel
        console.print(
            f"[cyan]Loading Whisper model {config.WHISPER_MODEL} ({config.WHISPER_COMPUTE_TYPE})…[/]"
        )
        _model = WhisperModel(
            config.WHISPER_MODEL,
            device="cuda",
            compute_type=config.WHISPER_COMPUTE_TYPE,
            download_root=str(config.MODELS),
        )
    return _model


def transcribe_video(video_id: int, audio_file: Path) -> int:
    """Transcribe one audio file; insert segments into DB. Returns segment count."""
    model = _get_model()
    # VAD params are tuned for casual speech in gaming recordings: we're ok
    # with a fairly aggressive onset since we'd rather cut a "hey!" than keep
    # 10s of engine noise.
    segments, info = model.transcribe(
        str(audio_file),
        language=None,         # autodetect — our recordings are English but robust anyway
        vad_filter=True,
        vad_parameters={"min_silence_duration_ms": 500},
        beam_size=5,
        condition_on_previous_text=False,  # avoids hallucination loops on repetitive audio
    )

    inserted = 0
    with db.db() as conn:
        conn.execute("DELETE FROM transcript_segment WHERE video_id = ?", (video_id,))
        total = info.duration or 0
        with Progress(
            TextColumn("[progress.description]{task.description}"),
            BarColumn(),
            TimeElapsedColumn(),
            TextColumn("{task.completed:.0f}/{task.total:.0f}s"),
            console=console,
        ) as prog:
            task = prog.add_task(f"Transcribing video_id={video_id}", total=total)
            for seg in segments:
                conn.execute(
                    """INSERT INTO transcript_segment
                        (video_id, start_s, end_s, text, avg_logprob, no_speech)
                        VALUES (?,?,?,?,?,?)""",
                    (
                        video_id,
                        float(seg.start),
                        float(seg.end),
                        seg.text.strip(),
                        float(seg.avg_logprob) if seg.avg_logprob is not None else None,
                        float(seg.no_speech_prob) if seg.no_speech_prob is not None else None,
                    ),
                )
                inserted += 1
                prog.update(task, completed=min(seg.end, total))
            prog.update(task, completed=total)

        conn.execute(
            "UPDATE video SET transcribe_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
    return inserted


def transcribe_pending(force: bool = False) -> int:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id FROM video
               WHERE audio_done = 1 AND (transcribe_done = 0 OR ? = 1)""",
            (1 if force else 0,),
        ).fetchall()
    total = 0
    for row in rows:
        audio_file = audio_mod.audio_path_for(row["id"])
        if not audio_file.exists():
            log.warning("missing audio for video_id=%s", row["id"])
            continue
        total += transcribe_video(row["id"], audio_file)
    return total
