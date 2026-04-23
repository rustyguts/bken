// POST /api/libraries/:id/sync
//
// Enqueue an immediate sync for a library; the cli worker picks it up.

import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid library id' })
  }

  const lib = db().prepare('SELECT * FROM library WHERE id = ?').get(id) as { name: string } | undefined
  if (!lib) {
    throw createError({ statusCode: 404, statusMessage: 'library not found' })
  }

  db().prepare(
    `INSERT INTO job (library_id, type, status) VALUES (?, ?, 'pending')`
  ).run(id, 'library_sync')

  return { ok: true, library_id: id }
})
