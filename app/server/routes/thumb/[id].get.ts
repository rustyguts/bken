// GET /thumb/:id.jpg
//
// On first request we shell out to ffmpeg to produce the jpeg; subsequent
// requests serve the cached file. If the underlying clip is missing we
// return a tiny 1×1 gray jpeg so <img> doesn't show a broken icon.

import { ensureThumb } from '~~/server/utils/thumbs'

// Minimal valid JPEG header + nulls. Just enough to keep the browser happy.
const PLACEHOLDER = Buffer.from([
  0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00, 0x08,
  ...new Array(60).fill(0),
])

export default defineEventHandler(async (event) => {
  const raw = event.context.params?.id ?? ''
  const id = Number(String(raw).replace(/\.jpg$/, ''))
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid clip id' })
  }

  const path = await ensureThumb(id)
  if (!path) {
    // eslint-disable-next-line no-console
    console.warn(`[thumb] no file for clip_id=${id} — returning placeholder`)
    setHeader(event, 'content-type', 'image/jpeg')
    return PLACEHOLDER
  }

  const file = Bun.file(path)
  return new Response(file, {
    headers: {
      'Content-Type': 'image/jpeg',
      'Cache-Control': 'public, max-age=3600',
    },
  })
})
