// Nitro plugin that schedules periodic library syncs using node-cron.
//
// On each tick we enqueue a `library_sync` row — the cli container's worker
// (`bken worker`) picks it up and runs the actual sync. The app container
// has no python and can't run the pipeline directly.

import { createRequire } from 'node:module'
import { db } from '~~/server/utils/db'

// node-cron ships a broken "esm" entry (CJS output served under the
// "import" condition); Bun's strict ESM loader chokes on its
// `__importDefault(events).default` pattern with "superclass is not a
// constructor". `createRequire` forces resolution via the CJS condition.
const cron = createRequire(import.meta.url)('node-cron') as typeof import('node-cron')

const scheduled = new Map<number, cron.ScheduledTask>()

function enqueueSync(libraryId: number) {
  // Skip if there's already pending/running work for this library — no
  // point stacking duplicates while the worker is busy.
  const pending = db()
    .prepare(
      `SELECT id FROM job
        WHERE library_id = ? AND type = 'library_sync'
          AND status IN ('pending', 'running')
        LIMIT 1`,
    )
    .get(libraryId) as { id: number } | undefined
  if (pending) return

  db().prepare(
    `INSERT INTO job (library_id, type, status) VALUES (?, 'library_sync', 'pending')`,
  ).run(libraryId)
}

function scheduleLibrary(library: {
  id: number
  scan_interval_minutes: number
  active: number
}) {
  // Cancel any existing schedule for this library
  const existing = scheduled.get(library.id)
  if (existing) {
    existing.stop()
    scheduled.delete(library.id)
  }

  if (!library.active) return

  const interval = library.scan_interval_minutes || 60
  // node-cron doesn't support minute intervals directly, so we use a cron
  // expression that runs every N minutes.
  const expression = `*/${Math.min(interval, 59)} * * * *`

  const task = cron.schedule(expression, () => {
    // Double-check the library is still active before enqueuing
    const row = db()
      .prepare('SELECT active FROM library WHERE id = ?')
      .get(library.id) as { active: number } | undefined
    if (row?.active) {
      enqueueSync(library.id)
    }
  }, { scheduled: true })

  scheduled.set(library.id, task)

  // Also kick off an initial sync at registration time so fresh dev
  // (or a new library) doesn't sit empty until the first cron tick.
  // enqueueSync is a no-op if a sync is already pending/running.
  enqueueSync(library.id)
}

function seedDefaultLibrary(): void {
  const path = process.env.BKEN_DEFAULT_LIBRARY
  if (!path) return
  const existing = db()
    .prepare('SELECT id FROM library WHERE path = ?')
    .get(path) as { id: number } | undefined
  if (existing) return
  const name = path.split('/').filter(Boolean).pop() || 'Default'
  db().prepare(
    'INSERT INTO library (name, path, scan_interval_minutes) VALUES (?, ?, ?)',
  ).run(name, path, 60)
  console.log(`[seed] default library "${name}" -> ${path}`)
}

export default defineNitroPlugin(() => {
  seedDefaultLibrary()

  const rows = db()
    .prepare('SELECT id, scan_interval_minutes, active FROM library WHERE active = 1')
    .all() as { id: number; scan_interval_minutes: number; active: number }[]

  for (const lib of rows) {
    scheduleLibrary(lib)
  }
})
