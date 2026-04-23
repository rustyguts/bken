// Nitro plugin that schedules periodic library syncs using node-cron.
//
// On startup it reads all active libraries and registers a cron job for each
// that spawns `bken library sync --library-id <id>`.  The interval is
// configurable per library (default 60 minutes).

import { createRequire } from 'node:module'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'
import { db } from '~~/server/utils/db'

// node-cron ships a broken "esm" entry (CJS output served under the
// "import" condition); Bun's strict ESM loader chokes on its
// `__importDefault(events).default` pattern with "superclass is not a
// constructor". `createRequire` forces resolution via the CJS condition.
const cron = createRequire(import.meta.url)('node-cron') as typeof import('node-cron')

const scheduled = new Map<number, cron.ScheduledTask>()

function spawnSync(libraryId: number) {
  const bkenPath = resolve(process.cwd(), '..', 'src', 'bken', 'cli.py')
  const proc = spawn('python', [bkenPath, 'library', 'sync', '--library-id', String(libraryId)], {
    cwd: resolve(process.cwd(), '..'),
    detached: true,
    stdio: 'ignore',
  })
  proc.unref()
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
    // Double-check the library is still active before spawning
    const row = db()
      .prepare('SELECT active FROM library WHERE id = ?')
      .get(library.id) as { active: number } | undefined
    if (row?.active) {
      spawnSync(library.id)
    }
  }, { scheduled: true })

  scheduled.set(library.id, task)
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
