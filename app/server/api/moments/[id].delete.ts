// DELETE /api/moments/:id
//
// Delete a moment and unlink its clip file if present.

import { existsSync, unlinkSync } from 'node:fs'
import { db } from '~~/server/utils/db'
import { resolveData } from '~~/server/utils/paths'

export default defineEventHandler((event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid moment id' })
  }

  const row = db()
    .prepare('SELECT clip_path FROM candidate_clip WHERE id = ?')
    .get(id) as { clip_path: string | null } | undefined

  if (!row) {
    throw createError({ statusCode: 404, statusMessage: 'moment not found' })
  }

  if (row.clip_path) {
    const path = resolveData(row.clip_path)
    if (existsSync(path)) {
      try {
        unlinkSync(path)
      } catch {
        // ignore
      }
    }
  }

  db().prepare('DELETE FROM candidate_clip WHERE id = ?').run(id)
  return { ok: true }
})
