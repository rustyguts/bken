"""bken — gaming video funny-moment extraction pipeline.

Modern Typer CLI for running the pipeline on individual files or whole
archives. Designed for archives with thousands of recordings:

    bken ingest /mnt/shack/media/gaming/archive --workers 8

ingests in parallel; downstream GPU stages (transcribe, detect-events)
run sequentially because they hold the GPU. The CPU/IO-bound stages
(audio extraction, clip cutting) parallelize via `--workers`.

Command groups:

  bken ingest|pipeline|...     stage commands (flat for muscle memory)
  bken videos ...              browse / inspect / reset indexed videos
  bken clips ...               browse / open candidate clips
  bken db ...                  schema init / vacuum / reset
  bken web                     (moved — see `nuxt/`; start with docker compose up)
  bken stats                   pipeline-wide overview
  bken config                  dump effective configuration
"""

from __future__ import annotations

import json
import logging
import os
import shutil
import subprocess
from pathlib import Path
from typing import Optional

import typer
from rich.console import Console
from rich.logging import RichHandler
from rich.panel import Panel
from rich.progress import (
    BarColumn,
    MofNCompleteColumn,
    Progress,
    SpinnerColumn,
    TextColumn,
    TimeElapsedColumn,
    TimeRemainingColumn,
)
from rich.table import Table

from . import audio as audio_mod
from . import clipper, config, db, events, ingest, library, ranker, scoring, transcribe, vision

# ──────────────────────────────────────────────────────────────────────────
# App + sub-apps
# ──────────────────────────────────────────────────────────────────────────

app = typer.Typer(
    add_completion=False,
    no_args_is_help=True,
    rich_markup_mode="rich",
    help="Extract funny / memorable moments from gameplay recordings.",
    context_settings={"help_option_names": ["-h", "--help"]},
)

videos_app = typer.Typer(
    no_args_is_help=True, help="Browse / inspect indexed videos.",
    rich_markup_mode="rich",
)
clips_app = typer.Typer(
    no_args_is_help=True, help="Browse / open candidate clips.",
    rich_markup_mode="rich",
)
db_app = typer.Typer(
    no_args_is_help=True, help="DB maintenance: init, vacuum, reset.",
    rich_markup_mode="rich",
)
libraries_app = typer.Typer(
    no_args_is_help=True, help="Manage watched library directories.",
    rich_markup_mode="rich",
)

app.add_typer(videos_app, name="videos")
app.add_typer(clips_app, name="clips")
app.add_typer(db_app, name="db")
app.add_typer(libraries_app, name="library")

console = Console()


# ──────────────────────────────────────────────────────────────────────────
# Common helpers
# ──────────────────────────────────────────────────────────────────────────


def _make_progress(*, transient: bool = False) -> Progress:
    """Standard Rich progress layout used by every stage."""
    return Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(bar_width=None),
        MofNCompleteColumn(),
        TextColumn("•"),
        TimeElapsedColumn(),
        TextColumn("•"),
        TimeRemainingColumn(),
        console=console,
        transient=transient,
    )


@app.callback()
def _root(
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Enable debug logging."),
    quiet: bool = typer.Option(False, "--quiet", "-q", help="Errors only."),
) -> None:
    """bken — gaming clip pipeline."""
    level = logging.WARNING
    if verbose:
        level = logging.DEBUG
    elif quiet:
        level = logging.ERROR
    else:
        level = logging.INFO
    logging.basicConfig(
        level=level,
        format="%(message)s",
        datefmt="[%X]",
        handlers=[RichHandler(console=console, rich_tracebacks=True, show_path=False)],
        force=True,
    )
    # Run any pending DB migrations so per-stage commands don't have to. Cheap;
    # `init_db` is idempotent — `CREATE … IF NOT EXISTS` plus column-existence
    # guards on the ALTER statements.
    if config.DB_PATH.exists():
        db.init_db()


# ──────────────────────────────────────────────────────────────────────────
# `bken library ...`
# ──────────────────────────────────────────────────────────────────────────


@libraries_app.command("add")
def cmd_library_add(
    path: Path = typer.Argument(..., exists=True, readable=True, help="Directory to watch."),
    name: Optional[str] = typer.Option(None, "--name", "-n", help="Display name (defaults to dir name)."),
    interval: int = typer.Option(60, "--interval", "-i", min=5, help="Scan interval in minutes."),
) -> None:
    """Add a new library directory."""
    db.init_db()
    lid = library.add_library(path, name=name, scan_interval_minutes=interval)
    console.print(f"[green]✓[/] Library [cyan]{name or path.name}[/] added (id={lid}).")
    console.print("  Run [cyan]bken library sync[/] to scan it now.")


@libraries_app.command("list")
def cmd_library_list() -> None:
    """List all libraries with video counts."""
    db.init_db()
    libs = library.list_libraries()
    if not libs:
        console.print("[yellow]No libraries configured.[/]")
        return
    t = Table("id", "name", "path", "interval", "videos", "missing", "last_scan",
              title="Libraries", header_style="bold cyan")
    for lib in libs:
        t.add_row(
            str(lib["id"]),
            lib["name"],
            lib["path"],
            f"{lib['scan_interval_minutes']}m",
            str(lib.get("video_count", 0)),
            str(lib.get("missing_count", 0)),
            lib.get("last_scan_at") or "—",
        )
    console.print(t)


@libraries_app.command("remove")
def cmd_library_remove(
    library_id: int = typer.Argument(..., help="Library ID to deactivate."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
) -> None:
    """Deactivate a library and mark its videos as missing.

    Files on disk are NEVER deleted.
    """
    db.init_db()
    lib = library.get_library(library_id)
    if not lib:
        console.print(f"[red]No library with id={library_id}.[/]")
        raise typer.Exit(1)
    if not yes:
        confirm = typer.confirm(
            f"Deactivate library '{lib['name']}' and mark its videos as missing?"
        )
        if not confirm:
            raise typer.Exit(0)
    library.remove_library(library_id)
    console.print(f"[green]✓[/] Library '{lib['name']}' deactivated. Videos marked missing.")


@libraries_app.command("delete")
def cmd_library_delete(
    library_id: int = typer.Argument(..., help="Library ID to permanently delete."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
) -> None:
    """Permanently delete a library and its video rows from the DB.

    Files on disk are NEVER deleted.
    """
    db.init_db()
    lib = library.get_library(library_id)
    if not lib:
        console.print(f"[red]No library with id={library_id}.[/]")
        raise typer.Exit(1)
    if not yes:
        confirm = typer.confirm(
            f"Permanently delete library '{lib['name']}' and all its video rows? "
            "This cannot be undone. Files on disk will NOT be deleted."
        )
        if not confirm:
            raise typer.Exit(0)
    library.delete_library(library_id)
    console.print(f"[green]✓[/] Library '{lib['name']}' deleted from DB.")


@libraries_app.command("sync")
def cmd_library_sync(
    library_id: Optional[int] = typer.Option(None, "--library-id", "-l", help="Sync a specific library."),
    dry_run: bool = typer.Option(False, "--dry-run", help="Show what would change without writing."),
    workers: int = typer.Option(8, "--workers", "-w", min=1, max=64),
) -> None:
    """Scan library directories and sync the DB.

    Unchanged files are skipped (size+mtime). New files are ingested.
    Missing files are marked missing. Moved files are detected via SHA1.
    """
    db.init_db()
    libs = library.list_libraries() if library_id is None else [library.get_library(library_id)]
    libs = [lib for lib in libs if lib]
    if not libs:
        console.print("[yellow]No libraries to sync.[/]")
        raise typer.Exit(0)

    for lib in libs:
        if not lib["active"]:
            console.print(f"[dim]Skipping inactive library '{lib['name']}'.[/]")
            continue

        console.rule(f"[bold cyan]Syncing library: {lib['name']}[/]")
        counts = library.sync_library(lib["id"], dry_run=dry_run)

        table = Table(title=f"Results for {lib['name']}", show_header=True, header_style="bold cyan")
        table.add_column("Status")
        table.add_column("Count", justify="right")
        table.add_row("[green]Inserted[/]", str(counts.get("inserted", 0)))
        table.add_row("[yellow]Updated[/]", str(counts.get("updated", 0)))
        table.add_row("[cyan]Restored[/]", str(counts.get("restored", 0)))
        table.add_row("[dim]Unchanged[/]", str(counts.get("unchanged", 0)))
        table.add_row("[red]Missing[/]", str(counts.get("missing", 0)))
        table.add_row("[red]Errored[/]", str(counts.get("errored", 0)))
        console.print(table)


# ──────────────────────────────────────────────────────────────────────────
# Top-level stage commands
# ──────────────────────────────────────────────────────────────────────────


@app.command("init")
def cmd_init() -> None:
    """Initialize the SQLite schema (idempotent)."""
    config.ensure_dirs()
    db.init_db()
    console.print(f"[green]✓[/] DB initialized at [cyan]{config.DB_PATH}[/]")


@app.command("ingest")
def cmd_ingest(
    target: Path = typer.Argument(
        ..., exists=True, readable=True,
        help="Video file, or a directory to walk recursively.",
    ),
    workers: int = typer.Option(
        8, "--workers", "-w", min=1, max=64,
        help="Parallel ffprobe + hashing workers.",
    ),
    single_file: bool = typer.Option(
        False, "--single-file", help="Treat TARGET as a single file (skip walk).",
    ),
    pattern: Optional[str] = typer.Option(
        None, "--pattern", "-p",
        help="fnmatch pattern applied to filenames (e.g. '2024-*.mp4').",
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Show what would be ingested, don't touch the DB.",
    ),
    limit: Optional[int] = typer.Option(
        None, "--limit", "-n", help="Cap to first N files (after pattern).",
    ),
) -> None:
    """Ingest a file or recursively scan a folder.

    Probes metadata with ffprobe, hashes the first 16 MB for change-detection,
    and inserts/updates one row per file. Re-running on an unchanged file is
    a no-op. Default 8 parallel workers — bump it for fast NVMe/local disks.
    """
    config.ensure_dirs()
    db.init_db()

    with console.status(f"[cyan]Scanning [bold]{target}[/]…[/]"):
        paths = ingest.collect_paths(target, single_file=single_file, pattern=pattern)
    if limit is not None:
        paths = paths[:limit]

    if not paths:
        console.print("[yellow]No video files found.[/]")
        raise typer.Exit(0)

    console.print(f"[cyan]Found [bold]{len(paths)}[/] video file(s).[/]")
    if dry_run:
        for p in paths[:50]:
            console.print(f"  {p}")
        if len(paths) > 50:
            console.print(f"  [dim]… and {len(paths) - 50} more[/]")
        raise typer.Exit(0)

    counts = {s: 0 for s in ingest.ALL_STATUSES}
    with _make_progress() as prog:
        task = prog.add_task(
            f"Ingesting (workers={workers})", total=len(paths)
        )

        def on_done(_path: Path, status: str) -> None:
            counts[status] = counts.get(status, 0) + 1
            prog.advance(task)
            prog.update(
                task,
                description=(
                    f"Ingesting (workers={workers}) "
                    f"[green]+{counts['inserted']}[/] "
                    f"[yellow]~{counts['updated']}[/] "
                    f"[dim]={counts['unchanged']}[/] "
                    f"[red]!{counts['errored']}[/]"
                ),
            )

        result = ingest.ingest_paths_parallel(paths, workers=workers, on_done=on_done)

    _print_ingest_summary(result, total=len(paths))


def _print_ingest_summary(counts: dict, total: int) -> None:
    table = Table(title="Ingest results", show_header=True, header_style="bold cyan")
    table.add_column("Status")
    table.add_column("Count", justify="right")
    table.add_row("[green]Inserted[/]", str(counts.get("inserted", 0)))
    table.add_row("[yellow]Updated[/]", str(counts.get("updated", 0)))
    table.add_row("[dim]Unchanged[/]", str(counts.get("unchanged", 0)))
    table.add_row("[red]Errored[/]", str(counts.get("errored", 0)))
    table.add_row("[bold]Total scanned[/]", str(total))
    console.print(table)


@app.command("extract-audio")
def cmd_extract_audio(
    force: bool = typer.Option(False, "--force", help="Re-decode even if cached."),
    workers: int = typer.Option(
        4, "--workers", "-w", min=1, max=32,
        help="Parallel ffmpeg processes.",
    ),
) -> None:
    """Decode each pending video to 16 kHz mono WAV. Cached on disk."""
    config.ensure_dirs()
    with db.db() as conn:
        n_pending = conn.execute(
            "SELECT COUNT(*) c FROM video WHERE audio_done = 0 OR ? = 1",
            (1 if force else 0,),
        ).fetchone()["c"]
    if n_pending == 0:
        console.print("[yellow]Nothing to do — every video already has cached audio.[/]")
        return

    with _make_progress() as prog:
        task = prog.add_task("Extracting audio", total=n_pending)
        ok_count = 0

        def on_done(_vid: int, ok: bool) -> None:
            nonlocal ok_count
            if ok:
                ok_count += 1
            prog.advance(task)

        audio_mod.extract_pending(force=force, workers=workers, on_done=on_done)
    console.print(f"[green]✓[/] Extracted audio for {ok_count}/{n_pending} video(s).")


@app.command("transcribe")
def cmd_transcribe(force: bool = typer.Option(False, "--force")) -> None:
    """Run faster-whisper on every audio-extracted video. (GPU; sequential.)"""
    with console.status("[cyan]Transcribing pending videos…[/]"):
        n = transcribe.transcribe_pending(force=force)
    console.print(f"[green]✓[/] {n} transcript segments inserted.")


@app.command("detect-events")
def cmd_detect_events(force: bool = typer.Option(False, "--force")) -> None:
    """Run PANNs sound-event detection + RMS spike detection. (GPU; sequential.)"""
    r = events.detect_pending(force=force)
    if not r:
        console.print("[yellow]Nothing to do.[/]")
        return
    table = Table("video_id", "Laughter", "Shout", "Cheer", "Gunfire", "LoudSpike",
                  title="Audio events detected", header_style="bold cyan")
    for vid, counts in r.items():
        table.add_row(
            str(vid),
            str(counts.get("Laughter", 0)),
            str(counts.get("Shout", 0)),
            str(counts.get("Cheer", 0)),
            str(counts.get("Gunfire", 0)),
            str(counts.get("LoudSpike", 0)),
        )
    console.print(table)


@app.command("detect-vision")
def cmd_detect_vision(
    force: bool = typer.Option(False, "--force"),
    fps: float = typer.Option(None, "--fps"),
    no_scene_detect: bool = typer.Option(False, "--no-scene-detect"),
    max_frames: int = typer.Option(None, "--max-frames"),
) -> None:
    """Per-second OCR + object detection + caption via Florence-2. (GPU; sequential.)"""
    # Guard against OptionInfo defaults when called programmatically.
    if isinstance(fps, (int, float)):
        config.VISION_FPS = float(fps)
    if isinstance(no_scene_detect, bool) and no_scene_detect:
        config.VISION_SCENE_DETECT = False
    if isinstance(max_frames, int):
        config.VISION_MAX_FRAMES = max_frames
    r = vision.detect_pending(force=force)
    if not r:
        console.print("[yellow]Nothing to do.[/]")
        return
    table = Table("video_id", "frames", "objects", "ocr",
                  title="Vision detected", header_style="bold cyan")
    for vid, counts in r.items():
        table.add_row(
            str(vid),
            str(counts.get("frames", 0)),
            str(counts.get("objects", 0)),
            str(counts.get("ocr", 0)),
        )
    console.print(table)


@app.command("vision")
def cmd_vision(
    action: str = typer.Argument(..., help="warmup"),
) -> None:
    """Vision subcommands: warmup — pre-download Florence-2 weights."""
    if action == "warmup":
        vision.warmup()
    else:
        console.print(f"[red]Unknown vision action:[/] {action}")
        raise typer.Exit(2)


@app.command("score")
def cmd_score(
    force: bool = typer.Option(False, "--force"),
    reset: bool = typer.Option(
        False, "--reset", "-R",
        help="DESTRUCTIVE: drop existing candidate_clip rows for affected videos "
             "before re-scoring. Wipes user_rating, llm_*, and clip_path references. "
             "Default behavior is a safe diff: matched clips keep their rating, "
             "title, and cached mp4; only their boundaries get refreshed.",
    ),
) -> None:
    """Density-based scoring → variable-length candidate clips per video.

    Re-running after editing scoring weights is safe by default — clips
    are matched to existing rows by IoU ≥ 0.5, so ratings and LLM titles
    survive. Pass --reset for the legacy DELETE+INSERT behavior.
    """
    label = "Re-scoring (RESET)" if reset else "Scoring"
    with console.status(f"[cyan]{label}…[/]"):
        r = scoring.score_pending(force=force, reset=reset)
    if not r:
        console.print("[yellow]Nothing to do.[/]")
        return
    for vid, n in r.items():
        console.print(f"  video_id={vid}: [green]{n}[/] active candidate clips")


@app.command("clip")
def cmd_clip(
    stream_copy: bool = typer.Option(
        False, "--stream-copy",
        help="Use -c copy for fast keyframe-boundary cuts (lossless, looser edges).",
    ),
    workers: int = typer.Option(
        4, "--workers", "-w", min=1, max=32,
        help="Parallel ffmpeg cut processes per video.",
    ),
    force: bool = typer.Option(
        False, "--force", "-f",
        help="Re-encode every clip in place using current config.CLIP_VIDEO_* settings, "
             "even if the output file already exists. Use after changing codec/CRF/preset.",
    ),
) -> None:
    """Cut candidate clips into mp4s with padding + clamping."""
    with db.db() as conn:
        plan = conn.execute(
            """SELECT v.id AS video_id,
                      (SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id) AS n_clips
                 FROM video v WHERE v.score_done=1"""
        ).fetchall()
    total_clips = sum(r["n_clips"] for r in plan)
    if total_clips == 0:
        console.print("[yellow]Nothing to cut.[/]")
        return

    with _make_progress() as prog:
        label = "Re-encoding clips" if force else "Cutting clips"
        task = prog.add_task(
            f"{label} (workers={workers})", total=total_clips
        )

        def on_clip_done(_vid: int, _clip_id: int, _ok: bool) -> None:
            prog.advance(task)

        results = clipper.extract_pending(
            stream_copy=stream_copy,
            workers=workers,
            on_clip_done=on_clip_done,
            force=force,
        )

    table = Table("video_id", "clips written", title="Clip extraction",
                  header_style="bold cyan")
    for vid, n in results.items():
        table.add_row(str(vid), str(n))
    console.print(table)


@app.command("rank")
def cmd_rank(force: bool = typer.Option(False, "--force")) -> None:
    """Re-rank candidate clips with the Claude CLI (Claude Code).

    No API key needed — uses your existing Claude subscription auth.
    Skipped silently if the `claude` CLI is not on PATH.
    """
    with console.status("[cyan]Asking Claude to rank clips…[/]"):
        r = ranker.rank_pending(force=force)
    if not r:
        console.print("[yellow]No videos ranked (missing CLI or none pending).[/]")
        return
    for vid, n in r.items():
        console.print(f"  video_id={vid}: [green]{n}[/] clips LLM-ranked")


@app.command("pipeline")
def cmd_pipeline(
    target: Path = typer.Argument(..., exists=True, readable=True),
    single_file: bool = typer.Option(False, "--single-file"),
    workers: int = typer.Option(
        8, "--workers", "-w", min=1, max=64,
        help="Workers for ingest + ffmpeg-bound stages.",
    ),
    pattern: Optional[str] = typer.Option(None, "--pattern", "-p"),
    skip_rank: bool = typer.Option(False, "--skip-rank", help="Don't shell out to Claude."),
    skip_vision: bool = typer.Option(False, "--skip-vision", help="Don't run Florence-2 vision stage."),
) -> None:
    """Run every stage end-to-end for the given path (stops before clip cutting).

    Ingest + audio stages run in parallel; transcribe + detect-events
    are GPU-bound so they run one video at a time.
    Clips are NOT cut — moments are surfaced in the UI for review.
    """
    config.ensure_dirs()

    console.rule("[bold cyan]ingest")
    cmd_ingest(
        target=target,
        workers=workers,
        single_file=single_file,
        pattern=pattern,
        dry_run=False,
        limit=None,
    )

    console.rule("[bold cyan]extract-audio")
    cmd_extract_audio(force=False, workers=max(2, min(workers, 8)))

    console.rule("[bold cyan]transcribe")
    cmd_transcribe(force=False)

    console.rule("[bold cyan]detect-events")
    cmd_detect_events(force=False)

    if not skip_vision:
        console.rule("[bold cyan]detect-vision")
        cmd_detect_vision(force=False)

    console.rule("[bold cyan]score")
    cmd_score(force=False)

    if not skip_rank:
        console.rule("[bold cyan]rank")
        cmd_rank(force=False)

    console.print(Panel.fit(
        "[green bold]Pipeline complete.[/]\n"
        "Moments identified — review and clip them in the web UI.",
        border_style="green",
    ))


@app.command("reprocess")
def cmd_reprocess(
    from_stage: str = typer.Argument(
        ...,
        help="Stage to reset + re-run from: audio|transcribe|events|vision|score|rank.",
    ),
    workers: int = typer.Option(
        8, "--workers", "-w", min=1, max=64,
        help="Workers for ffmpeg-bound stages.",
    ),
    skip_rank: bool = typer.Option(False, "--skip-rank", help="Don't shell out to Claude."),
    skip_vision: bool = typer.Option(False, "--skip-vision", help="Don't run Florence-2 vision stage."),
    stream_copy: bool = typer.Option(False, "--stream-copy"),
    force_clip: bool = typer.Option(
        False, "--force-clip",
        help="Re-encode every existing clip in place (useful after changing codec/CRF).",
    ),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation prompt."),
) -> None:
    """Reset stage flags for ALL indexed videos and re-run from `from_stage` onward.

    Useful after tuning scoring weights, changing vision config, switching clip
    codecs, etc.  Each stage only processes videos whose flag is 0, so this is
    effectively a bulk "start over from here".

    Examples:
        bken reprocess score              # re-score + re-clip + re-rank
        bken reprocess vision --skip-rank # re-run vision, score, clip
        bken reprocess audio --yes        # full reprocess from scratch
    """
    valid_stages = {"audio", "transcribe", "events", "vision", "score", "rank"}
    if from_stage not in valid_stages:
        console.print(f"[red]Unknown stage:[/] {from_stage}. One of {sorted(valid_stages)}.")
        raise typer.Exit(2)

    with db.db() as conn:
        total = conn.execute("SELECT COUNT(*) c FROM video").fetchone()["c"]
    if total == 0:
        console.print("[yellow]No videos indexed — nothing to reprocess.[/]")
        raise typer.Exit(0)

    if not yes:
        confirm = typer.confirm(
            f"Reset ALL {total} indexed video(s) from '{from_stage}' and re-run the pipeline?"
        )
        if not confirm:
            raise typer.Exit(0)

    console.rule(f"[bold yellow]Resetting {total} video(s) from {from_stage}")
    count = ingest.reset_all_stages(from_stage)
    console.print(f"[green]✓[/] Reset flags for {count} video(s).")

    # Run stages from `from_stage` onward, mirroring cmd_pipeline.
    if from_stage == "audio":
        console.rule("[bold cyan]extract-audio")
        cmd_extract_audio(force=True, workers=max(2, min(workers, 8)))

    if from_stage in {"audio", "transcribe"}:
        console.rule("[bold cyan]transcribe")
        cmd_transcribe(force=False)

    if from_stage in {"audio", "transcribe", "events"}:
        console.rule("[bold cyan]detect-events")
        cmd_detect_events(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision"} and not skip_vision:
        console.rule("[bold cyan]detect-vision")
        cmd_detect_vision(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision", "score"}:
        console.rule("[bold cyan]score")
        cmd_score(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision", "score"}:
        console.rule("[bold cyan]clip")
        cmd_clip(stream_copy=stream_copy, workers=max(2, min(workers, 8)), force=force_clip)

    if from_stage in {"audio", "transcribe", "events", "vision", "score", "rank"} and not skip_rank:
        console.rule("[bold cyan]rank")
        cmd_rank(force=False)

    console.print(Panel.fit(
        "[green bold]Reprocess complete.[/]\n"
        "Moments re-identified — review and clip them in the web UI.",
        border_style="green",
    ))


@app.command("reprocess-video")
def cmd_reprocess_video(
    video_id: int = typer.Argument(..., help="Video ID to reprocess."),
    from_stage: str = typer.Argument(
        "score",
        help="Stage to reset + re-run from: audio|transcribe|events|vision|score|rank.",
    ),
    workers: int = typer.Option(
        4, "--workers", "-w", min=1, max=32,
        help="Workers for ffmpeg-bound stages.",
    ),
    skip_rank: bool = typer.Option(False, "--skip-rank", help="Don't shell out to Claude."),
    skip_vision: bool = typer.Option(False, "--skip-vision", help="Don't run Florence-2 vision stage."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation prompt."),
) -> None:
    """Reset stage flags for ONE video and re-run from `from_stage` onward."""
    valid_stages = {"audio", "transcribe", "events", "vision", "score", "rank"}
    if from_stage not in valid_stages:
        console.print(f"[red]Unknown stage:[/] {from_stage}. One of {sorted(valid_stages)}.")
        raise typer.Exit(2)

    v = ingest.get_video(video_id)
    if not v:
        console.print(f"[red]No video with id={video_id}.[/]")
        raise typer.Exit(1)

    if not yes:
        confirm = typer.confirm(
            f"Reprocess video_id={video_id} ({Path(v['path']).name}) from '{from_stage}'?"
            f"\nAuto-generated moments may change; manual moments are preserved."
        )
        if not confirm:
            raise typer.Exit(0)

    ingest.reset_video_stages(video_id, [from_stage])
    console.print(f"[green]✓[/] Reset {from_stage} for video_id={video_id}.")

    if from_stage == "audio":
        console.rule("[bold cyan]extract-audio")
        audio_mod.extract_pending(force=True, workers=max(2, min(workers, 8)))

    if from_stage in {"audio", "transcribe"}:
        console.rule("[bold cyan]transcribe")
        cmd_transcribe(force=False)

    if from_stage in {"audio", "transcribe", "events"}:
        console.rule("[bold cyan]detect-events")
        cmd_detect_events(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision"} and not skip_vision:
        console.rule("[bold cyan]detect-vision")
        cmd_detect_vision(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision", "score"}:
        console.rule("[bold cyan]score")
        cmd_score(force=False)

    if from_stage in {"audio", "transcribe", "events", "vision", "score", "rank"} and not skip_rank:
        console.rule("[bold cyan]rank")
        cmd_rank(force=False)

    console.print(Panel.fit(
        f"[green bold]Reprocess complete for video_id={video_id}.[/]\n"
        "Review updated moments in the web UI.",
        border_style="green",
    ))


@app.command("create-clip")
def cmd_create_clip(
    clip_id: int = typer.Argument(..., help="candidate_clip ID to cut."),
    stream_copy: bool = typer.Option(False, "--stream-copy"),
    force: bool = typer.Option(False, "--force", "-f"),
) -> None:
    """Cut a single candidate clip to disk."""
    with console.status(f"[cyan]Cutting clip {clip_id}…[/]"):
        ok = clipper.cut_single(clip_id, stream_copy=stream_copy, force=force)
    if ok:
        console.print(f"[green]✓[/] Clip {clip_id} created.")
    else:
        console.print(f"[red]✗[/] Failed to create clip {clip_id}.")
        raise typer.Exit(1)


# ──────────────────────────────────────────────────────────────────────────
# `bken videos ...`
# ──────────────────────────────────────────────────────────────────────────


@videos_app.command("list")
def cmd_videos_list(
    game: Optional[str] = typer.Option(None, "--game", "-g", help="Filter by game (parent dir name)."),
    pending: Optional[str] = typer.Option(
        None, "--pending",
        help="Show only videos pending a stage: audio|transcribe|events|score|rank.",
    ),
) -> None:
    """Tabular dashboard of indexed videos + per-stage completion."""
    vids = ingest.list_videos()
    if game:
        vids = [v for v in vids if (v.get("game") or "").lower() == game.lower()]
    flag_map = {
        "audio": "audio_done", "transcribe": "transcribe_done",
        "events": "events_done", "vision": "vision_done",
        "score": "score_done", "rank": "rank_done",
    }
    if pending:
        if pending not in flag_map:
            console.print(f"[red]Unknown stage:[/] {pending}. One of {list(flag_map)}.")
            raise typer.Exit(2)
        vids = [v for v in vids if not v[flag_map[pending]]]

    if not vids:
        console.print("[yellow]No videos match.[/]")
        return

    t = Table(
        "id", "name", "game", "dur", "vcodec", "acodec",
        "aud", "txn", "evt", "vsn", "scr", "rnk",
        title=f"Videos ({len(vids)})", header_style="bold cyan",
    )
    for v in vids:
        t.add_row(
            str(v["id"]),
            Path(v["path"]).name,
            (v.get("game") or "-")[:20],
            f"{v['duration_s']/60:.1f}m" if v["duration_s"] else "-",
            v["video_codec"] or "-",
            v["audio_codec"] or "-",
            "[green]Y[/]" if v["audio_done"] else "[dim].[/]",
            "[green]Y[/]" if v["transcribe_done"] else "[dim].[/]",
            "[green]Y[/]" if v["events_done"] else "[dim].[/]",
            "[green]Y[/]" if v.get("vision_done") else "[dim].[/]",
            "[green]Y[/]" if v["score_done"] else "[dim].[/]",
            "[green]Y[/]" if v["rank_done"] else "[dim].[/]",
        )
    console.print(t)


@videos_app.command("show")
def cmd_videos_show(video_id: int) -> None:
    """Print full metadata for one video."""
    v = ingest.get_video(video_id)
    if not v:
        console.print(f"[red]No video with id={video_id}.[/]")
        raise typer.Exit(1)
    t = Table.grid(padding=(0, 2))
    t.add_column(style="cyan")
    t.add_column()
    for k in (
        "id", "path", "game", "recorded_at", "duration_s",
        "width", "height", "fps", "video_codec", "audio_codec",
        "audio_channels", "audio_rate", "size_bytes", "sha1_prefix",
        "audio_done", "transcribe_done", "events_done", "score_done", "rank_done",
        "created_at", "updated_at",
    ):
        t.add_row(k, str(v.get(k)))
    console.print(Panel(t, title=f"video_id={video_id}", border_style="cyan"))


@videos_app.command("reset")
def cmd_videos_reset(
    video_id: int,
    stage: list[str] = typer.Argument(
        ..., help="Stages to clear: audio transcribe events score rank (any subset).",
    ),
) -> None:
    """Mark a video's stages as not-done so they re-run."""
    try:
        ingest.reset_video_stages(video_id, stage)
    except ValueError as e:
        console.print(f"[red]{e}[/]")
        raise typer.Exit(2)
    console.print(f"[green]✓[/] Reset {stage} for video_id={video_id}.")


@videos_app.command("delete")
def cmd_videos_delete(
    video_id: int,
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
) -> None:
    """Remove a video row, cached audio, and its clip directory.

    The source mp4 on disk is *not* touched.
    """
    v = ingest.get_video(video_id)
    if not v:
        console.print(f"[red]No video with id={video_id}.[/]")
        raise typer.Exit(1)
    if not yes:
        confirm = typer.confirm(
            f"Delete video_id={video_id} ({Path(v['path']).name}) "
            "and its cached audio + clips? (Source file is kept.)"
        )
        if not confirm:
            raise typer.Exit(0)
    ingest.delete_video(video_id)
    console.print(f"[green]✓[/] Deleted video_id={video_id}.")


# ──────────────────────────────────────────────────────────────────────────
# `bken clips ...`
# ──────────────────────────────────────────────────────────────────────────


@clips_app.command("show")
def cmd_clips_show(
    video_id: int,
    limit: Optional[int] = typer.Option(None, "--limit", "-n"),
    include_inactive: bool = typer.Option(
        False, "--include-inactive", "-a",
        help="Also show clips deactivated by a later re-score.",
    ),
) -> None:
    """Pretty-print candidate clips for a video, ordered by LLM rank then heuristic score."""
    sql = "SELECT * FROM candidate_clip WHERE video_id=?"
    if not include_inactive:
        sql += " AND active=1"
    sql += " ORDER BY COALESCE(llm_rank, 999), score DESC"
    with db.db() as conn:
        rows = conn.execute(sql, (video_id,)).fetchall()
    if not rows:
        console.print("[yellow]No clips for that video_id.[/]")
        return
    if limit:
        rows = rows[:limit]
    for r in rows:
        tags = ""
        if r["llm_tags"]:
            try:
                tags = " ".join(f"#{t}" for t in json.loads(r["llm_tags"]))
            except json.JSONDecodeError:
                pass
        llm_rank = f"#{r['llm_rank']}" if r["llm_rank"] else "-"
        title = r["llm_title"] or ""
        active_marker = "" if r["active"] else " [red](inactive)[/]"
        rating = f" {'★' * (r['user_rating'] or 0)}" if r["user_rating"] else ""
        console.print(
            f"[bold]{llm_rank}[/] [cyan]{r['start_s']:7.0f}-{r['end_s']:7.0f}s "
            f"({r['end_s']-r['start_s']:4.0f}s)[/] "
            f"score={r['score']:6.2f}  [green]{title}[/]{rating}{active_marker}  [dim]{tags}[/]"
        )
        if r["llm_desc"]:
            console.print(f"    {r['llm_desc']}")
        console.print(f"    [dim]{(r['transcript'] or '')[:200]}[/]")
        if r["clip_path"]:
            console.print(f"    [blue]{r['clip_path']}[/]")
        console.print()


@clips_app.command("open")
def cmd_clips_open(clip_id: int) -> None:
    """Open a clip in the system default video player (xdg-open)."""
    with db.db() as conn:
        row = conn.execute(
            "SELECT clip_path FROM candidate_clip WHERE id=?", (clip_id,)
        ).fetchone()
    if not row or not row["clip_path"]:
        console.print(f"[red]No clip with id={clip_id} (or not yet cut).[/]")
        raise typer.Exit(1)
    path = row["clip_path"]
    opener = "xdg-open" if shutil.which("xdg-open") else "open"
    subprocess.Popen([opener, path], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    console.print(f"[green]✓[/] Opened [cyan]{path}[/]")


@clips_app.command("top")
def cmd_clips_top(
    n: int = typer.Option(20, "--limit", "-n"),
    game: Optional[str] = typer.Option(None, "--game", "-g"),
    include_inactive: bool = typer.Option(
        False, "--include-inactive", "-a",
        help="Also show clips deactivated by a later re-score.",
    ),
) -> None:
    """Top N clips overall, by user rating then LLM rank then heuristic score."""
    sql = """
        SELECT c.*, v.game, v.path AS vpath
        FROM candidate_clip c JOIN video v ON v.id=c.video_id
        WHERE c.clip_path IS NOT NULL
    """
    if not include_inactive:
        sql += " AND c.active=1"
    params: list = []
    if game:
        sql += " AND v.game = ?"
        params.append(game)
    sql += """ ORDER BY COALESCE(c.user_rating, 0) DESC,
                        COALESCE(c.llm_rank, 999) ASC,
                        c.score DESC LIMIT ?"""
    params.append(n)
    with db.db() as conn:
        rows = conn.execute(sql, params).fetchall()

    if not rows:
        console.print("[yellow]No cut clips yet.[/]")
        return
    t = Table(
        "id", "vid", "game", "rank", "★", "score", "title", "tags",
        title=f"Top {len(rows)} clips", header_style="bold cyan",
    )
    for r in rows:
        tags = ""
        if r["llm_tags"]:
            try:
                tags = " ".join(f"#{x}" for x in json.loads(r["llm_tags"])[:3])
            except json.JSONDecodeError:
                pass
        t.add_row(
            str(r["id"]),
            str(r["video_id"]),
            (r["game"] or "-")[:18],
            str(r["llm_rank"] or "-"),
            "★" * (r["user_rating"] or 0) or "-",
            f"{r['score']:.1f}",
            (r["llm_title"] or "")[:40],
            tags[:32],
        )
    console.print(t)


@clips_app.command("rate")
def cmd_clips_rate(
    clip_id: int,
    rating: int = typer.Argument(..., min=0, max=5, help="0 to clear, 1-5 stars."),
) -> None:
    """Set a clip's user rating (0 clears it)."""
    with db.db() as conn:
        row = conn.execute(
            "SELECT id FROM candidate_clip WHERE id=?", (clip_id,)
        ).fetchone()
        if not row:
            console.print(f"[red]No clip with id={clip_id}.[/]")
            raise typer.Exit(1)
        new = None if rating == 0 else rating
        conn.execute(
            "UPDATE candidate_clip SET user_rating=? WHERE id=?", (new, clip_id)
        )
    console.print(f"[green]✓[/] clip {clip_id} rated {rating}.")


@clips_app.command("prune")
def cmd_clips_prune(
    dry_run: bool = typer.Option(False, "--dry-run", help="Show what would be deleted."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
) -> None:
    """Delete inactive clips that have no rating and no LLM data.

    Re-scoring marks rows that no longer surface as `active=0` instead of
    deleting them, so user ratings and LLM titles aren't lost when you
    tune weights. This command cleans up the truly-orphaned rows: ones
    that were deactivated, never rated, and never seen by the LLM
    reranker. The mp4 + thumb files are unlinked too.
    """
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id, clip_path, thumb_path FROM candidate_clip
               WHERE active=0 AND user_rating IS NULL
                 AND llm_title IS NULL AND llm_contents IS NULL"""
        ).fetchall()
    if not rows:
        console.print("[green]Nothing to prune.[/]")
        return

    console.print(f"[yellow]Would delete {len(rows)} inactive clip(s) "
                  f"with no rating and no LLM data.[/]")
    if dry_run:
        for r in rows[:20]:
            console.print(f"  id={r['id']}  [dim]{r['clip_path'] or '(no mp4)'}[/]")
        if len(rows) > 20:
            console.print(f"  [dim]… and {len(rows) - 20} more[/]")
        return

    if not yes:
        confirm = typer.confirm("Proceed?")
        if not confirm:
            raise typer.Exit(0)

    unlinked = 0
    for r in rows:
        for col in ("clip_path", "thumb_path"):
            p = r[col]
            if not p:
                continue
            path = config.resolve_data(p)
            if path.exists():
                try:
                    path.unlink()
                    unlinked += 1
                except OSError as e:
                    console.print(f"  [red]could not unlink {path}: {e}[/]")
    with db.db() as conn:
        conn.executemany(
            "DELETE FROM candidate_clip WHERE id=?",
            [(r["id"],) for r in rows],
        )
    console.print(
        f"[green]✓[/] Deleted {len(rows)} clip rows, unlinked {unlinked} file(s)."
    )


# ──────────────────────────────────────────────────────────────────────────
# `bken db ...`
# ──────────────────────────────────────────────────────────────────────────


@db_app.command("init")
def cmd_db_init() -> None:
    """Create the schema (idempotent)."""
    cmd_init()


@db_app.command("vacuum")
def cmd_db_vacuum() -> None:
    """Run SQLite VACUUM to compact the DB file."""
    with console.status("[cyan]VACUUM…[/]"):
        with db.db() as conn:
            conn.execute("VACUUM")
    console.print(f"[green]✓[/] Vacuumed [cyan]{config.DB_PATH}[/]")


@db_app.command("reset")
def cmd_db_reset(
    yes: bool = typer.Option(False, "--yes", "-y"),
) -> None:
    """Delete the entire DB file (NOT cached audio/clips). DESTRUCTIVE."""
    if not yes:
        confirm = typer.confirm(
            f"Delete {config.DB_PATH}? All ingest/transcript/event/clip metadata is lost."
        )
        if not confirm:
            raise typer.Exit(0)
    if config.DB_PATH.exists():
        config.DB_PATH.unlink()
    for ext in ("-wal", "-shm"):
        side = config.DB_PATH.with_name(config.DB_PATH.name + ext)
        if side.exists():
            side.unlink()
    console.print("[green]✓[/] DB reset. Run [cyan]bken db init[/] to recreate.")


# ──────────────────────────────────────────────────────────────────────────
# Misc top-level
# ──────────────────────────────────────────────────────────────────────────


@app.command("list")
def cmd_list_alias(
    game: Optional[str] = typer.Option(None, "--game", "-g"),
    pending: Optional[str] = typer.Option(None, "--pending"),
) -> None:
    """Alias for `bken videos list`."""
    cmd_videos_list(game=game, pending=pending)


@app.command("show-clips")
def cmd_show_clips_alias(
    video_id: int,
    limit: Optional[int] = typer.Option(None, "--limit", "-n"),
) -> None:
    """Alias for `bken clips show <video_id>`."""
    cmd_clips_show(video_id=video_id, limit=limit)


@app.command("web")
def cmd_web() -> None:
    """The web UI lives in the Nuxt app now — the Python server has been retired."""
    console.print(Panel.fit(
        "[bold yellow]The web UI has moved to the Nuxt project in [cyan]nuxt/[/].\n\n"
        "[white]Dev:[/]   [cyan]docker compose up[/]  →  http://localhost:3000\n"
        "[white]Local:[/] [cyan]cd nuxt && bun run dev[/]  →  http://localhost:3000",
        border_style="yellow",
    ))
    raise typer.Exit(0)


@app.command("stats")
def cmd_stats() -> None:
    """Pipeline-wide stats: video / segment / event / clip totals."""
    with db.db() as conn:
        v = conn.execute(
            """SELECT COUNT(*) c,
                      COALESCE(SUM(duration_s), 0) total_dur,
                      SUM(audio_done) ad, SUM(transcribe_done) td,
                      SUM(events_done) ed, SUM(vision_done) vd,
                      SUM(score_done) sd, SUM(rank_done) rd
                 FROM video"""
        ).fetchone()
        n_seg = conn.execute("SELECT COUNT(*) c FROM transcript_segment").fetchone()["c"]
        n_evt = conn.execute("SELECT COUNT(*) c FROM audio_event").fetchone()["c"]
        n_vf = conn.execute("SELECT COUNT(*) c FROM vision_frame").fetchone()["c"]
        n_vd = conn.execute("SELECT COUNT(*) c FROM vision_detection").fetchone()["c"]
        n_vo = conn.execute("SELECT COUNT(*) c FROM vision_ocr").fetchone()["c"]
        n_clip_active = conn.execute(
            "SELECT COUNT(*) c FROM candidate_clip WHERE active=1"
        ).fetchone()["c"]
        n_clip_inactive = conn.execute(
            "SELECT COUNT(*) c FROM candidate_clip WHERE active=0"
        ).fetchone()["c"]
        n_cut = conn.execute(
            "SELECT COUNT(*) c FROM candidate_clip WHERE clip_path IS NOT NULL AND active=1"
        ).fetchone()["c"]
        n_rated = conn.execute(
            "SELECT COUNT(*) c FROM candidate_clip WHERE user_rating IS NOT NULL"
        ).fetchone()["c"]
        # Length distribution — surfaces whether variable-length scoring is working.
        len_stats = conn.execute(
            """SELECT MIN(end_s - start_s) min_l,
                      AVG(end_s - start_s) avg_l,
                      MAX(end_s - start_s) max_l
                 FROM candidate_clip WHERE active=1"""
        ).fetchone()
        per_game = conn.execute(
            """SELECT game, COUNT(*) n, COALESCE(SUM(duration_s),0) dur
                 FROM video WHERE game IS NOT NULL
                 GROUP BY game ORDER BY n DESC LIMIT 10"""
        ).fetchall()

    total = v["c"] or 0
    dur_h = (v["total_dur"] or 0) / 3600

    overview = Table.grid(padding=(0, 2))
    overview.add_column(style="cyan", justify="right")
    overview.add_column()
    overview.add_row("Videos", f"{total}")
    overview.add_row("Total duration", f"{dur_h:.1f} hours")
    overview.add_row("audio done",     f"{v['ad'] or 0} / {total}")
    overview.add_row("transcribe done",f"{v['td'] or 0} / {total}")
    overview.add_row("events done",    f"{v['ed'] or 0} / {total}")
    overview.add_row("vision done",    f"{v['vd'] or 0} / {total}")
    overview.add_row("score done",     f"{v['sd'] or 0} / {total}")
    overview.add_row("rank done",      f"{v['rd'] or 0} / {total}")
    overview.add_row("Transcript segs", f"{n_seg}")
    overview.add_row("Audio events",    f"{n_evt}")
    overview.add_row("Vision frames",   f"{n_vf}")
    overview.add_row("Vision detections", f"{n_vd}")
    overview.add_row("Vision OCR",      f"{n_vo}")
    overview.add_row("Candidate clips", f"{n_clip_active} active / {n_clip_inactive} inactive")
    if len_stats and len_stats["avg_l"] is not None:
        overview.add_row(
            "Active clip length",
            f"min {len_stats['min_l']:.0f}s / "
            f"avg {len_stats['avg_l']:.0f}s / "
            f"max {len_stats['max_l']:.0f}s",
        )
    overview.add_row("Clips on disk",   f"{n_cut}")
    overview.add_row("Rated clips",     f"{n_rated}")

    console.print(Panel(overview, title="[bold cyan]bken stats", border_style="cyan"))

    if per_game:
        t = Table("game", "videos", "hours",
                  title="Top games by recording count", header_style="bold cyan")
        for g in per_game:
            t.add_row(g["game"], str(g["n"]), f"{(g['dur'] or 0)/3600:.1f}")
        console.print(t)


@app.command("config")
def cmd_config() -> None:
    """Dump the effective configuration (paths, model, weights)."""
    t = Table.grid(padding=(0, 2))
    t.add_column(style="cyan", justify="right")
    t.add_column()
    rows = [
        ("ROOT", config.ROOT),
        ("DATA", config.DATA),
        ("MODELS", config.MODELS),
        ("DB_PATH", config.DB_PATH),
        ("AUDIO_DIR", config.AUDIO_DIR),
        ("CLIPS_DIR", config.CLIPS_DIR),
        ("ARTIFACTS_DIR", config.ARTIFACTS_DIR),
        ("AUDIO_SAMPLE_RATE", config.AUDIO_SAMPLE_RATE),
        ("AUDIO_CHANNELS", config.AUDIO_CHANNELS),
        ("WHISPER_MODEL", config.WHISPER_MODEL),
        ("WHISPER_COMPUTE_TYPE", config.WHISPER_COMPUTE_TYPE),
        ("TOP_N_CANDIDATES", config.TOP_N_CANDIDATES),
        ("CLIP_PAD_BEFORE / AFTER", f"{config.CLIP_PAD_BEFORE} / {config.CLIP_PAD_AFTER}"),
        ("CLIP_MIN / MAX", f"{config.CLIP_MIN_SECONDS} / {config.CLIP_MAX_SECONDS}"),
        ("VISION_FPS", config.VISION_FPS),
        ("VISION_SCENE_DETECT", config.VISION_SCENE_DETECT),
        ("VISION_SCENE_THRESHOLD", config.VISION_SCENE_THRESHOLD),
        ("VISION_MAX_FRAMES", config.VISION_MAX_FRAMES),
        ("VISION_BATCH_SIZE", config.VISION_BATCH_SIZE),
        ("VISION_SCORE_ENABLED", config.VISION_SCORE_ENABLED),
        ("BKEN_CLAUDE_CLI", os.environ.get("BKEN_CLAUDE_CLI", "claude")),
        ("BKEN_CLAUDE_MODEL", os.environ.get("BKEN_CLAUDE_MODEL", "sonnet")),
    ]
    for k, val in rows:
        t.add_row(str(k), str(val))
    console.print(Panel(t, title="[bold cyan]bken config", border_style="cyan"))


if __name__ == "__main__":
    app()
