// GET /video/:id/stream.mp4
//
// Streams the original source video with HTTP-Range support.

import { db } from '~~/server/utils/db'

interface Row {
  id: number
  path: string | null
}

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw.replace(/\.mp4$/, '').replace(/\/stream$/, ''))
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const row = db()
    .prepare('SELECT id, path FROM video WHERE id=?')
    .get(id) as Row | undefined
  if (!row || !row.path) {
    throw createError({ statusCode: 404, statusMessage: 'video not found' })
  }

  const path = row.path
  const file = Bun.file(path)
  if (!await file.exists()) {
    throw createError({ statusCode: 404, statusMessage: 'video file missing' })
  }

  return new Response(file, {
    headers: {
      'Content-Type': 'video/mp4',
      'Accept-Ranges': 'bytes',
    },
  })
})
