// POST /api/rate/:id?rating=N
//
// 0 = clear (NULL), 1..5 = star rating. Returns the updated clip DTO.

import { CLIP_SELECT, clipToDto, db, type ClipRow } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const id = Number(event.context.params?.id)
  if (!Number.isFinite(id)) {
    throw createError({ statusCode: 400, statusMessage: 'invalid clip id' })
  }

  const rating = Math.max(0, Math.min(5, Number(getQuery(event).rating ?? 0) || 0))

  const conn = db()
  const exists = conn.prepare('SELECT 1 FROM candidate_clip WHERE id=?').get(id)
  if (!exists) throw createError({ statusCode: 404, statusMessage: 'clip not found' })

  const newValue = rating === 0 ? null : rating
  conn.prepare('UPDATE candidate_clip SET user_rating=? WHERE id=?').run(newValue, id)

  const row = conn.prepare(`${CLIP_SELECT} WHERE c.id=?`).get(id) as ClipRow
  return clipToDto(row)
})
