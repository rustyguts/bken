// GET /video/:id/stream.mp4
//
// Streams the original source video with HTTP-Range support.

import { createReadStream, existsSync, statSync } from 'node:fs'
import { db } from '~~/server/utils/db'

interface Row {
  id: number
  path: string | null
}

export default defineEventHandler((event) => {
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
  if (!existsSync(path)) {
    throw createError({ statusCode: 404, statusMessage: 'video file missing' })
  }

  const stat = statSync(path)
  const total = stat.size

  setHeader(event, 'content-type', 'video/mp4')
  setHeader(event, 'accept-ranges', 'bytes')

  const range = getRequestHeader(event, 'range')
  if (range) {
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
      setResponseStatus(event, 416)
      setHeader(event, 'content-range', `bytes */${total}`)
      return ''
    }
  }

  setHeader(event, 'content-length', String(total))
  return sendStream(event, createReadStream(path))
})
