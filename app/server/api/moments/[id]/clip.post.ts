// POST /api/moments/:id/clip
//
// Enqueue an ffmpeg cut for a single moment; the cli worker picks it up
// and runs `bken create-clip <id> --force`.

import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid moment id' })
  }

  const row = db()
    .prepare('SELECT video_id FROM candidate_clip WHERE id = ?')
    .get(id) as { video_id: number } | undefined
  if (!row) {
    throw createError({ statusCode: 404, statusMessage: 'moment not found' })
  }

  db().prepare(
    `INSERT INTO job (video_id, type, status, target_id) VALUES (?, 'create_clip', 'pending', ?)`
  ).run(row.video_id, id)

  // Optimistically clear the stale flag so the UI shows Download/Share as
  // soon as the user clicks Update. If the worker job fails, the existing
  // clip_path is still pointing at the old cut — that's acceptable for
  // dev; prod would retry or surface the failure.
  db().prepare('UPDATE candidate_clip SET clip_stale = 0 WHERE id = ?').run(id)

  return { ok: true, moment_id: id }
})
