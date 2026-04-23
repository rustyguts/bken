// POST /api/libraries/:id/sync
//
// Trigger an immediate sync for a library.

import { db } from '~~/server/utils/db'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'

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

  const vidpipePath = resolve(process.cwd(), '..', 'src', 'vidpipe', 'cli.py')
  const proc = spawn('python', [vidpipePath, 'library', 'sync', '--library-id', String(id)], {
    cwd: resolve(process.cwd(), '..'),
    detached: true,
    stdio: 'ignore',
  })
  proc.unref()

  return { ok: true, library_id: id }
})
