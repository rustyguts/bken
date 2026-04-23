// PATCH /api/moments/:id
//
// Update a moment's title, start_s, or end_s.

import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid moment id' })
  }

  const body = await readBody(event)
  const updates: string[] = []
  const params: any[] = []

  if (body.title !== undefined) {
    updates.push('title = ?')
    params.push(String(body.title).trim() || null)
  }
  if (body.start_s !== undefined) {
    updates.push('start_s = ?')
    params.push(Number(body.start_s))
  }
  if (body.end_s !== undefined) {
    updates.push('end_s = ?')
    params.push(Number(body.end_s))
  }
  // Trimming a moment invalidates its existing clip file — the UI will
  // force an Update click before the Download/Share links reappear.
  if (body.start_s !== undefined || body.end_s !== undefined) {
    updates.push('clip_stale = CASE WHEN clip_path IS NOT NULL THEN 1 ELSE 0 END')
  }

  if (updates.length === 0) {
    throw createError({ statusCode: 400, statusMessage: 'no fields to update' })
  }

  params.push(id)
  db().prepare(`UPDATE candidate_clip SET ${updates.join(', ')} WHERE id = ?`).run(...params)

  return { ok: true }
})
