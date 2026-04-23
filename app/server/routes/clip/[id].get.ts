// GET /clip/:id.mp4
//
// Streams the cut mp4 with HTTP-Range support so the HTML <video> tag can
// scrub without downloading the whole file. `?download=1` tacks on a
// Content-Disposition header with a friendlier filename.

import { createReadStream, existsSync, statSync } from 'node:fs'
import { db } from '~~/server/utils/db'
import { resolveData } from '~~/server/utils/paths'

interface Row {
  id: number
  start_s: number | null
  game: string | null
  recorded_at: string | null
  clip_path: string | null
}

function downloadFilename(r: Row): string {
  const game = (r.game || 'clip').trim() || 'clip'
  const date = (r.recorded_at || '').slice(0, 10) || 'undated'
  const start = Math.floor(Number(r.start_s ?? 0))
  const mmss = `${String(Math.floor(start / 60)).padStart(2, '0')}${String(start % 60).padStart(2, '0')}`
  return `${game}_${date}_${mmss}_clip${r.id}.mp4`.replace(/[^A-Za-z0-9._-]+/g, '_')
}

export default defineEventHandler((event) => {
  // File-based route captures `4.mp4` from `/clip/4.mp4` — strip the ext.
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw.replace(/\.mp4$/, ''))
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid clip id' })
  }

  const row = db()
    .prepare(
      `SELECT c.id, c.start_s, c.clip_path, v.game, v.recorded_at
         FROM candidate_clip c JOIN video v ON v.id = c.video_id
        WHERE c.id=?`,
    )
    .get(id) as Row | undefined
  if (!row || !row.clip_path) {
    throw createError({ statusCode: 404, statusMessage: 'clip not found' })
  }
  const path = resolveData(row.clip_path)
  if (!existsSync(path)) {
    throw createError({ statusCode: 404, statusMessage: 'clip file missing' })
  }

  const stat = statSync(path)
  const total = stat.size

  setHeader(event, 'content-type', 'video/mp4')
  setHeader(event, 'accept-ranges', 'bytes')

  if (getQuery(event).download) {
    setHeader(
      event,
      'content-disposition',
      `attachment; filename="${downloadFilename(row)}"`,
    )
  }

  const range = getRequestHeader(event, 'range')
  if (range) {
    // Spec is `bytes=START-END` (END optional). We clamp to the file size.
    const m = /^bytes=(\d*)-(\d*)$/.exec(range)
    if (m) {
      const CHUNK_SIZE = 10 ** 6 // 1MB
      const start = m[1] === '' ? Math.max(total - Number(m[2]), 0) : Number(m[1])
      let end = m[2] === '' || m[1] === '' ? total - 1 : Number(m[2])
      end = Math.min(end, start + CHUNK_SIZE - 1, total - 1)
      if (start >= 0 && start <= end && end < total) {
        setResponseStatus(event, 206)
        setHeader(event, 'content-range', `bytes ${start}-${end}/${total}`)
        setHeader(event, 'content-length', String(end - start + 1))
        return sendStream(event, createReadStream(path, { start, end }))
      }
      // Unsatisfiable range → 416.
      setResponseStatus(event, 416)
      setHeader(event, 'content-range', `bytes */${total}`)
      return ''
    }
  }

  setHeader(event, 'content-length', String(total))
  return sendStream(event, createReadStream(path))
})
