// POST /api/moments
//
// Create a manual moment for a video.

import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const body = await readBody(event)
  const videoId = Number(body?.video_id)
  const startS = Number(body?.start_s)
  const endS = Number(body?.end_s)
  const title = String(body?.title || '').trim() || null

  if (!Number.isFinite(videoId) || videoId <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video_id' })
  }
  if (!Number.isFinite(startS) || !Number.isFinite(endS) || endS <= startS) {
    throw createError({ statusCode: 400, statusMessage: 'invalid start/end times' })
  }

  const result = db()
    .prepare(
      `INSERT INTO candidate_clip
       (video_id, start_s, end_s, score, features, transcript, active, title, source)
       VALUES (?, ?, ?, 0, '{}', '', 1, ?, 'manual')`,
    )
    .run(videoId, startS, endS, title)

  return { ok: true, id: result.lastInsertRowid }
})
