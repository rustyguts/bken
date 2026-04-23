// POST /api/videos/:id/reprocess
//
// Reset stage flags for a video and enqueue reprocessing jobs.

import { db } from '~~/server/utils/db'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const body = await readBody(event)
  const fromStage = body?.from_stage || 'score'
  const validStages = ['audio', 'transcribe', 'events', 'vision', 'score', 'rank']
  if (!validStages.includes(fromStage)) {
    throw createError({ statusCode: 400, statusMessage: `invalid stage: ${fromStage}` })
  }

  // Insert job record
  db().prepare(
    `INSERT INTO job (video_id, type, status) VALUES (?, ?, 'pending')`
  ).run(id, `reprocess:${fromStage}`)

  // Spawn reprocess in background so the API returns immediately
  const bkenPath = resolve(process.cwd(), '..', 'src', 'bken', 'cli.py')
  const args = ['reprocess-video', String(id), fromStage, '--yes']
  const proc = spawn('python', [bkenPath, ...args], {
    cwd: resolve(process.cwd(), '..'),
    detached: true,
    stdio: 'ignore',
  })
  proc.unref()

  return { ok: true, video_id: id, from_stage: fromStage }
})
