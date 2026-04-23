"""Re-rank + caption candidate clips by shelling out to the Claude CLI.

The heuristic scorer surfaces up to ~30 candidate moments per video using
audio and transcript signals; that's good for recall, but the decision
of *what is actually funny* is better handed to a language model reading
the dialogue.

This module uses the local `claude` CLI (Claude Code) rather than the
Anthropic HTTP SDK. That means no `ANTHROPIC_API_KEY` is needed — the
user's existing Claude subscription authentication handles it — at the
cost of a subprocess per video. For a personal archive the extra wall
time is fine.

Claude is prompted with all candidates for one video in a single call
and asked to return a JSON array. We parse the JSON and write rank /
title / description / tags back onto each `candidate_clip` row.
"""

from __future__ import annotations

import json
import logging
import os
import re
import shutil
import subprocess
from pathlib import Path

from rich.console import Console

from . import db

log = logging.getLogger(__name__)
console = Console()

CLI_BIN = os.environ.get("BKEN_CLAUDE_CLI", "claude")
# Sonnet is the right default for this task: curation-quality judgment matters
# more than throughput (one call per video, and the encoder stage dominates
# wall time). Override to "haiku" or a pinned ID via `BKEN_CLAUDE_MODEL`.
MODEL_FLAG = os.environ.get("BKEN_CLAUDE_MODEL", "sonnet")

PROMPT_PREAMBLE = """You are helping curate a personal gaming-clip archive from
years of gameplay recordings with friends. Below is a batch of candidate
moments from one session. A heuristic scorer picked them using laughter,
shouting, excited keywords, loud audio events, and on-screen visual signals
(death screens like "WASTED" / "YOU DIED", victory banners, crash footage,
OCR text, and object detections) — so every excerpt is a *candidate*, not
guaranteed to be a hit.

For each excerpt, judge whether the combined audio + visual context suggests a
genuinely funny, memorable, cool, or interesting moment (the kind someone would
later say "remember that time…" about). Strong visual signals (death screens,
victory text, explosions visible on screen) are just as important as dialogue.
Ignore generic chatter, mission narration, NPC voice lines, or empty reaction
sounds unless the visuals make them notable.

Return a JSON array, one object per excerpt, in the same order you received
them, each with:
  - "clip_id": integer, same as given
  - "rank": integer 1..N, 1 = best (must be a permutation of 1..N)
  - "keep": boolean, true if worth keeping in the curated set
  - "title": short punchy title, ≤ 8 words
  - "reason": one sentence explaining *why* it's worth keeping (or not)
  - "contents": 2-3 sentences narrating what actually happens in the clip
  - "tags": array of 1-5 short tags (e.g. "laugh", "crash", "callout", "argument")

Return ONLY the JSON array, no prose, no markdown fences.

=== CANDIDATES ===
"""


def _vision_ocr_for_clip(conn, video_id: int, start_s: float, end_s: float) -> list[str]:
    rows = conn.execute(
        """SELECT text, text_upper, area_frac FROM vision_ocr
           WHERE video_id=? AND ts_s BETWEEN ? AND ?
           ORDER BY CASE WHEN text_upper LIKE '%WASTED%' OR text_upper LIKE '%DIED%' OR text_upper LIKE '%VICTORY%' THEN 0 ELSE 1 END,
                    area_frac DESC
           LIMIT 10""",
        (video_id, start_s, end_s),
    ).fetchall()
    seen: set[str] = set()
    out: list[str] = []
    for r in rows:
        key = r["text_upper"].strip()
        if key and key not in seen:
            seen.add(key)
            out.append(r["text"].strip())
    return out


def _vision_caption_for_clip(conn, video_id: int, start_s: float, end_s: float) -> str:
    peak = (start_s + end_s) / 2
    row = conn.execute(
        "SELECT caption FROM vision_frame WHERE video_id=? AND ts_s BETWEEN ? AND ? ORDER BY ABS(ts_s - ?) LIMIT 1",
        (video_id, start_s, end_s, peak),
    ).fetchone()
    return (row["caption"] if row else "") or ""


def _build_prompt(candidates: list[dict]) -> str:
    # Batch vision lookups per video_id to avoid N+1, but we need per-clip ranges.
    # For simplicity, query inside the loop — candidate counts are small (~30).
    parts = [PROMPT_PREAMBLE]
    with db.db() as conn:
        for c in candidates:
            parts.append(
                f"--- clip_id={c['id']}  t={c['start_s']:.0f}-{c['end_s']:.0f}s  "
                f"heuristic_score={c['score']:.2f} ---"
            )
            feat = c.get("features") or ""
            if feat:
                parts.append(f"signals: {feat}")
            ocr = _vision_ocr_for_clip(conn, c["video_id"], c["start_s"], c["end_s"])
            caption = _vision_caption_for_clip(conn, c["video_id"], c["start_s"], c["end_s"])
            if ocr:
                parts.append(f"on-screen text: {', '.join(ocr)}")
            if caption:
                parts.append(f"peak caption: {caption[:240]}")
            parts.append((c.get("transcript") or "").strip() or "(no transcript)")
            parts.append("")
    return "\n".join(parts)


def _extract_json_array(text: str) -> list[dict]:
    # Tolerate ```json fences, stray prose, etc.
    m = re.search(r"\[\s*\{.*?\}\s*\]", text, re.DOTALL)
    if not m:
        raise ValueError(f"no JSON array in model response: {text[:400]}")
    return json.loads(m.group(0))


def _run_claude_cli(prompt: str, cwd: Path | None = None) -> str:
    """Invoke the Claude Code CLI in print mode and return its stdout.

    Uses stdin for the prompt to avoid argv length limits on long batches.
    """
    cmd = [CLI_BIN, "-p"]
    if MODEL_FLAG:
        cmd += ["--model", MODEL_FLAG]
    result = subprocess.run(
        cmd,
        input=prompt,
        capture_output=True,
        text=True,
        cwd=str(cwd) if cwd else None,
        timeout=300,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"claude CLI failed (rc={result.returncode}): {result.stderr[:500]}"
        )
    return result.stdout


def rank_video(video_id: int) -> int:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id, start_s, end_s, score, features, transcript
               FROM candidate_clip WHERE video_id=? AND active=1
               ORDER BY score DESC""",
            (video_id,),
        ).fetchall()
    candidates = [dict(r) for r in rows]
    if not candidates:
        return 0

    prompt = _build_prompt(candidates)
    console.print(f"[cyan]Calling `claude` for video_id={video_id} "
                  f"({len(candidates)} candidates, {len(prompt)} chars)…[/]")

    stdout = _run_claude_cli(prompt)
    try:
        results = _extract_json_array(stdout)
    except (ValueError, json.JSONDecodeError) as e:
        log.error("LLM response parse error: %s\n---stdout---\n%s", e, stdout[:800])
        return 0

    results_by_id = {r["clip_id"]: r for r in results if "clip_id" in r}
    n = 0
    with db.db() as conn:
        for c in candidates:
            r = results_by_id.get(c["id"])
            if not r:
                continue
            conn.execute(
                """UPDATE candidate_clip
                   SET llm_rank=?, llm_title=?, llm_desc=?, llm_contents=?, llm_tags=?
                   WHERE id=?""",
                (
                    int(r.get("rank")) if r.get("rank") is not None else None,
                    (r.get("title") or "").strip()[:200],
                    (r.get("reason") or r.get("description") or "").strip()[:1000],
                    (r.get("contents") or "").strip()[:2000],
                    json.dumps(r.get("tags") or []),
                    c["id"],
                ),
            )
            n += 1
        conn.execute(
            "UPDATE video SET rank_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
    return n


def rank_pending(force: bool = False) -> dict[int, int]:
    if shutil.which(CLI_BIN) is None:
        console.print(f"[yellow]`{CLI_BIN}` CLI not on PATH — skipping LLM rank stage.[/]")
        return {}
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id FROM video
               WHERE score_done = 1 AND (rank_done = 0 OR ? = 1)""",
            (1 if force else 0,),
        ).fetchall()
    return {r["id"]: rank_video(r["id"]) for r in rows}
