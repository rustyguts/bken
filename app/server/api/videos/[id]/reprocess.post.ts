// POST /api/videos/:id/reprocess
//
// Enqueue a reprocess job; the cli worker runs `bken reprocess-video`.

import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const body = await readBody(event)
  const fromStage = body?.from_stage || 'score'
  const validStages = ['audio', 'transcribe', 'events', 'vision', 'score', 'rank']
  if (!validStages.includes(fromStage)) {
    throw createError({ statusCode: 400, statusMessage: `invalid stage: ${fromStage}` })
  }

  db().prepare(
    `INSERT INTO job (video_id, type, status) VALUES (?, ?, 'pending')`
  ).run(id, `reprocess:${fromStage}`)

  return { ok: true, video_id: id, from_stage: fromStage }
})
