// POST /api/moments/:id/clip
//
// Trigger ffmpeg cut for a single moment.

import { spawn } from 'node:child_process'
import { resolve } from 'node:path'
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

  // Insert job record
  db().prepare(
    `INSERT INTO job (video_id, type, status, target_id) VALUES (?, 'create_clip', 'pending', ?)`
  ).run(row.video_id, id)

  // Optimistically clear the stale flag so the UI shows Download/Share
  // as soon as the user clicks Update. The Python job runs asynchronously;
  // if it fails, the existing clip_path is still pointing at the old cut.
  db().prepare('UPDATE candidate_clip SET clip_stale = 0 WHERE id = ?').run(id)

  // Spawn clip creation in background. --force overwrites the existing cut
  // so start/end edits actually replace the previous clip.
  const bkenPath = resolve(process.cwd(), '..', 'src', 'bken', 'cli.py')
  const proc = spawn('python', [bkenPath, 'create-clip', String(id), '--force'], {
    cwd: resolve(process.cwd(), '..'),
    detached: true,
    stdio: 'ignore',
  })
  proc.unref()

  return { ok: true, moment_id: id }
})
