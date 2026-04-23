"""Job-queue worker.

The Nuxt app container writes rows to the `job` table but can't execute
anything pipeline-related (no python, no ffmpeg GPU). This worker runs
inside the `cli` container, polls the queue, and shells out to the existing
`bken` subcommands — keeping one source of truth for each stage's logic.

Failure isolates per-job: an exception marks that row failed and the loop
continues. The worker is single-threaded on purpose — SQLite WAL is fine
for one writer, and the heavy jobs (reprocess, sync) already saturate disk
and GPU on their own.
"""

from __future__ import annotations

import logging
import subprocess
import time
from typing import Optional

from . import db

log = logging.getLogger(__name__)

POLL_INTERVAL_S = 3.0


def _claim_next() -> Optional[dict]:
    """Atomically pick up the oldest pending job, flipping it to running."""
    with db.db() as conn:
        conn.execute("BEGIN IMMEDIATE")
        row = conn.execute(
            "SELECT id, type, video_id, library_id, target_id FROM job "
            "WHERE status='pending' ORDER BY id ASC LIMIT 1"
        ).fetchone()
        if not row:
            conn.execute("COMMIT")
            return None
        conn.execute(
            "UPDATE job SET status='running', updated_at=datetime('now') WHERE id=?",
            (row["id"],),
        )
        conn.execute("COMMIT")
        return dict(row)


def _mark(job_id: int, status: str, error: Optional[str] = None) -> None:
    with db.db() as conn:
        conn.execute(
            "UPDATE job SET status=?, error_message=?, updated_at=datetime('now') "
            "WHERE id=?",
            (status, error, job_id),
        )


def _dispatch(job: dict) -> None:
    """Shell out to the matching `bken` subcommand."""
    jtype = job["type"]

    if jtype == "create_clip":
        target = job["target_id"]
        if target is None:
            raise ValueError("create_clip job missing target_id")
        subprocess.run(["bken", "create-clip", str(target), "--force"], check=True)
        return

    if jtype == "library_sync":
        lib_id = job["library_id"]
        if lib_id is None:
            raise ValueError("library_sync job missing library_id")
        subprocess.run(
            ["bken", "library", "sync", "--library-id", str(lib_id)], check=True
        )
        return

    if jtype.startswith("reprocess:"):
        stage = jtype.split(":", 1)[1]
        video_id = job["video_id"]
        if video_id is None:
            raise ValueError("reprocess job missing video_id")
        subprocess.run(
            ["bken", "reprocess-video", str(video_id), stage, "--yes"], check=True
        )
        return

    raise ValueError(f"unknown job type: {jtype}")


def run_forever() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [worker] %(message)s",
        datefmt="%H:%M:%S",
    )
    log.info("started — polling every %.1fs", POLL_INTERVAL_S)
    db.init_db()

    while True:
        try:
            job = _claim_next()
        except Exception as e:
            log.error("poll failed: %s", e)
            time.sleep(POLL_INTERVAL_S)
            continue

        if job is None:
            time.sleep(POLL_INTERVAL_S)
            continue

        log.info("job #%d (%s) starting", job["id"], job["type"])
        try:
            _dispatch(job)
            _mark(job["id"], "done")
            log.info("job #%d done", job["id"])
        except subprocess.CalledProcessError as e:
            msg = f"exit {e.returncode} from {' '.join(e.cmd)}"
            _mark(job["id"], "failed", msg)
            log.error("job #%d FAILED: %s", job["id"], msg)
        except Exception as e:
            _mark(job["id"], "failed", str(e))
            log.exception("job #%d FAILED", job["id"])
