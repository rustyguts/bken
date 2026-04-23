// DELETE /api/libraries/:id
//
// Deactivate a library and mark its videos as missing.

import { db } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid library id' })
  }

  const lib = db().prepare('SELECT * FROM library WHERE id = ?').get(id) as { name: string } | undefined
  if (!lib) {
    throw createError({ statusCode: 404, statusMessage: 'library not found' })
  }

  db().prepare('UPDATE library SET active = 0 WHERE id = ?').run(id)
  db().prepare(
    'UPDATE video SET missing = 1, updated_at = datetime(\'now\') WHERE library_id = ?'
  ).run(id)

  return { ok: true, id, name: lib.name }
})
