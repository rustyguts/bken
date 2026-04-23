"""Summarize + rank candidate clips with a small local LLM.

The heuristic scorer surfaces up to ~30 candidate moments per video from
audio + transcript + vision signals. This module asks a small instruct-tuned
model (default: Qwen2.5-1.5B-Instruct, override via `BKEN_RANK_MODEL`) to
write a short title / reason / contents / tags per clip. Weights download to
`config.MODELS` on first use — no external API, fully offline after that.

`llm_rank` is derived from the existing heuristic `score` order (DESC) after
summarization. Small LLMs are unreliable at producing a well-formed 30-item
global permutation, and the UI's default sort (`llm_rank ASC NULLS LAST,
score DESC`) reproduces the same ordering we'd write anyway.
"""

from __future__ import annotations

import json
import logging
import re
from typing import Any, Optional

from rich.console import Console

from . import config, db

log = logging.getLogger(__name__)
console = Console()

# Lazy-loaded singletons. Torch and transformers are heavy imports; keeping
# them inside `_load_model` means `bken --help` and non-rank commands stay
# fast. Module globals cache the load across `rank_pending` → `rank_video`
# calls within one process.
_model: Any = None
_tokenizer: Any = None


SYSTEM_PROMPT = (
    "You curate a personal gaming-clip archive. For each candidate moment "
    "you see (audio transcript + visual signals), decide whether it's a "
    "memorable, funny, or exciting moment and summarize it.\n\n"
    "Output ONLY a single JSON object, no prose, no markdown fences. "
    "Required keys:\n"
    "- \"title\": short punchy title, <= 8 words\n"
    "- \"reason\": one sentence explaining why it's memorable (or not)\n"
    "- \"contents\": 2-3 sentences narrating what actually happens\n"
    "- \"tags\": array of 1-5 short tags like \"laugh\", \"crash\", "
    "\"callout\", \"death\", \"kill\""
)


def _load_model():
    """Download (first run) + load the HF chat model + tokenizer."""
    global _model, _tokenizer
    if _model is not None:
        return _model, _tokenizer

    # Deferred so the import cost (torch + transformers ≈ 2-3s) is only paid
    # when we actually need to rank.
    from transformers import AutoModelForCausalLM, AutoTokenizer

    model_id = config.RANK_MODEL
    console.print(
        f"[cyan]Loading ranker model [bold]{model_id}[/] "
        f"(cache: {config.MODELS})…[/]"
    )
    _tokenizer = AutoTokenizer.from_pretrained(
        model_id, cache_dir=str(config.MODELS)
    )
    _model = AutoModelForCausalLM.from_pretrained(
        model_id,
        cache_dir=str(config.MODELS),
        torch_dtype="auto",
        device_map="auto",
    )
    _model.eval()
    dev = next(_model.parameters()).device
    console.print(f"[cyan]Ranker loaded on [bold]{dev}[/].[/]")
    return _model, _tokenizer


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


def _build_user_prompt(clip: dict, ocr: list[str], caption: str) -> str:
    duration = clip["end_s"] - clip["start_s"]
    lines = [
        f"Clip @ {clip['start_s']:.0f}-{clip['end_s']:.0f}s "
        f"({duration:.0f}s), heuristic_score={clip['score']:.2f}.",
    ]
    feat = (clip.get("features") or "").strip()
    if feat:
        lines.append(f"signals: {feat}")
    if ocr:
        lines.append(f"on-screen text: {', '.join(ocr)}")
    if caption:
        lines.append(f"peak visual: {caption[:240]}")
    transcript = (clip.get("transcript") or "").strip()
    lines.append(f"transcript: {transcript or '(no speech)'}")
    lines.append("")
    lines.append("Return the JSON object now.")
    return "\n".join(lines)


def _extract_json_object(text: str) -> dict:
    """Pull the first balanced {...} block out of a model response.

    Tolerates stray prose, ``` fences, and trailing outputs — small models
    often include one or the other even when told not to.
    """
    text = re.sub(r"```(?:json)?", "", text)
    start = text.find("{")
    if start == -1:
        raise ValueError(f"no JSON object in response: {text[:200]!r}")
    depth = 0
    in_str = False
    esc = False
    for i in range(start, len(text)):
        ch = text[i]
        if in_str:
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
            continue
        if ch == '"':
            in_str = True
        elif ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return json.loads(text[start : i + 1])
    raise ValueError(f"unbalanced JSON in response: {text[start:][:200]!r}")


def _generate(messages: list[dict]) -> str:
    model, tokenizer = _load_model()
    import torch

    inputs = tokenizer.apply_chat_template(
        messages,
        add_generation_prompt=True,
        return_tensors="pt",
        tokenize=True,
    ).to(model.device)
    with torch.inference_mode():
        out = model.generate(
            inputs,
            max_new_tokens=400,
            do_sample=False,
            pad_token_id=tokenizer.eos_token_id,
        )
    # Only the newly-generated tokens (strip the echoed prompt).
    new_tokens = out[0][inputs.shape[-1] :]
    return tokenizer.decode(new_tokens, skip_special_tokens=True)


def _rank_one_clip(clip: dict, ocr: list[str], caption: str) -> Optional[dict]:
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": _build_user_prompt(clip, ocr, caption)},
    ]
    raw = _generate(messages)
    try:
        return _extract_json_object(raw)
    except (ValueError, json.JSONDecodeError) as e:
        log.warning(
            "clip_id=%s: couldn't parse model response (%s): %r",
            clip["id"], e, raw[:200],
        )
        return None


def rank_video(video_id: int) -> int:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id, video_id, start_s, end_s, score, features, transcript
               FROM candidate_clip WHERE video_id=? AND active=1
               ORDER BY score DESC""",
            (video_id,),
        ).fetchall()
    candidates = [dict(r) for r in rows]
    if not candidates:
        return 0

    console.print(
        f"[cyan]Ranking video_id={video_id} ({len(candidates)} clips)…[/]"
    )

    summarized = 0
    with db.db() as conn:
        for idx, c in enumerate(candidates, 1):
            ocr = _vision_ocr_for_clip(conn, c["video_id"], c["start_s"], c["end_s"])
            caption = _vision_caption_for_clip(
                conn, c["video_id"], c["start_s"], c["end_s"]
            )
            result = _rank_one_clip(c, ocr, caption)
            if result is not None:
                conn.execute(
                    """UPDATE candidate_clip
                       SET llm_title=?, llm_desc=?, llm_contents=?, llm_tags=?
                       WHERE id=?""",
                    (
                        (result.get("title") or "").strip()[:200],
                        (result.get("reason") or result.get("description") or "").strip()[:1000],
                        (result.get("contents") or "").strip()[:2000],
                        json.dumps(result.get("tags") or []),
                        c["id"],
                    ),
                )
                summarized += 1
                title = (result.get("title") or "").strip()[:60]
                console.print(f"  [green]✓[/] [dim]{idx}/{len(candidates)}[/] {title}")
            else:
                console.print(
                    f"  [yellow]∅[/] [dim]{idx}/{len(candidates)}[/] "
                    f"clip_id={c['id']} (parse failed)"
                )

        # llm_rank is assigned from heuristic-score order regardless of parse
        # success, so the UI always has a stable sort key.
        for rank, c in enumerate(candidates, 1):
            conn.execute(
                "UPDATE candidate_clip SET llm_rank=? WHERE id=?",
                (rank, c["id"]),
            )

        conn.execute(
            "UPDATE video SET rank_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
    return summarized


def rank_pending(force: bool = False) -> dict[int, int]:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id FROM video
               WHERE score_done = 1 AND (rank_done = 0 OR ? = 1)""",
            (1 if force else 0,),
        ).fetchall()
    return {r["id"]: rank_video(r["id"]) for r in rows}
