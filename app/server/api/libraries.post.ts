import { stat } from 'node:fs/promises'
import { resolve } from 'node:path'
import { db } from '~~/server/utils/db'

export default defineEventHandler(async (event) => {
  const body = await readBody(event)
  const path = body?.path
  const name = body?.name || null
  const interval = body?.scan_interval_minutes ?? 60

  if (!path || typeof path !== 'string') {
    throw createError({ statusCode: 400, statusMessage: 'path is required' })
  }

  // Check path exists
  try {
    const s = await stat(path)
    if (!s.isDirectory()) {
      throw createError({ statusCode: 400, statusMessage: 'path must be a directory' })
    }
  } catch {
    throw createError({ statusCode: 400, statusMessage: 'path does not exist or is not accessible' })
  }

  const resolved = resolve(path)

  // Insert library
  const existing = db().prepare('SELECT id FROM library WHERE path = ?').get(resolved) as { id: number } | undefined
  if (existing) {
    throw createError({ statusCode: 409, statusMessage: 'library already exists for this path' })
  }

  const result = db().prepare(
    'INSERT INTO library (name, path, scan_interval_minutes) VALUES (?, ?, ?)'
  ).run(name || resolved.split('/').pop() || 'Library', resolved, interval)

  const libraryId = Number(result.lastInsertRowid)

  // Enqueue the initial sync — the cli container's worker picks it up.
  db().prepare(
    `INSERT INTO job (library_id, type, status) VALUES (?, ?, 'pending')`
  ).run(libraryId, 'library_sync')

  return { ok: true, id: libraryId }
})
