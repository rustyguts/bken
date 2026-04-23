// GET /clip/:id.mp4
//
// Streams the cut mp4 with HTTP-Range support so the HTML <video> tag can
// scrub without downloading the whole file. `?download=1` tacks on a
// Content-Disposition header with a friendlier filename.

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

export default defineEventHandler(async (event) => {
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
  const path = await resolveData(row.clip_path)
  const file = Bun.file(path)
  if (!await file.exists()) {
    throw createError({ statusCode: 404, statusMessage: 'clip file missing' })
  }

  const headers: Record<string, string> = {
    'Content-Type': 'video/mp4',
    'Accept-Ranges': 'bytes',
  }

  if (getQuery(event).download) {
    headers['Content-Disposition'] = `attachment; filename="${downloadFilename(row)}"`
  }

  // Returning a Response directly to Nitro. Bun's Response constructor
  // automatically handles the 'Range' request header if the body is a BunFile.
  return new Response(file, { headers })
})
