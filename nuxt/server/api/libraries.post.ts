// POST /api/libraries
//
// Create a new library and enqueue an initial sync.

import { db } from '~~/server/utils/db'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'

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
    const stat = await import('node:fs').then((fs) => fs.promises.stat(path))
    if (!stat.isDirectory()) {
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

  // Insert sync job
  db().prepare(
    `INSERT INTO job (library_id, type, status) VALUES (?, ?, 'pending')`
  ).run(libraryId, 'library_sync')

  // Spawn sync in background
  const vidpipePath = resolve(process.cwd(), '..', 'src', 'vidpipe', 'cli.py')
  const proc = spawn('python', [vidpipePath, 'library', 'sync', '--library-id', String(libraryId)], {
    cwd: resolve(process.cwd(), '..'),
    detached: true,
    stdio: 'ignore',
  })
  proc.unref()

  return { ok: true, id: libraryId }
})
