import { createReadStream, existsSync, mkdirSync, statSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { db } from '~~/server/utils/db'
import { THUMBS_DIR, resolveData } from '~~/server/utils/paths'

const THUMB_WIDTH = 640

const PLACEHOLDER = Buffer.from([
  0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00, 0x08,
  ...new Array(60).fill(0),
])

async function ensureVideoThumb(videoId: number): Promise<string | null> {
  const out = resolve(THUMBS_DIR, `video_${String(videoId).padStart(6, '0')}.jpg`)
  if (existsSync(out) && statSync(out).size > 0) return out

  const row = db()
    .prepare('SELECT path, duration_s FROM video WHERE id=?')
    .get(videoId) as { path: string | null; duration_s: number | null } | undefined
  if (!row || !row.path) return null

  const videoFile = resolveData(row.path)
  if (!existsSync(videoFile)) return null

  // Extract at 10% of the duration or 5s, so we don't just get a black frame
  const dur = row.duration_s || 30
  const seek = Math.max(1.0, dur * 0.1)

  mkdirSync(dirname(out), { recursive: true })

  const vf = [
    'setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709:range=tv',
    `scale=${THUMB_WIDTH}:-2:flags=lanczos:in_color_matrix=bt709:out_color_matrix=bt601`,
    'format=yuvj420p',
  ].join(',')

  await new Promise<void>((res, rej) => {
    const p = spawn('ffmpeg', [
      '-y',
      '-hide_banner',
      '-loglevel',
      'error',
      '-ss',
      seek.toFixed(3),
      '-i',
      videoFile,
      '-frames:v',
      '1',
      '-vf',
      vf,
      '-q:v',
      '3',
      out,
    ])
    let stderr = ''
    p.stderr.on('data', (chunk) => (stderr += chunk))
    p.on('error', rej)
    p.on('exit', (code) => {
      if (code === 0) res()
      else rej(new Error(`ffmpeg exit ${code}: ${stderr.slice(0, 400)}`))
    })
  })

  return out
}

export default defineEventHandler(async (event) => {
  const raw = event.context.params?.id ?? ''
  const id = Number(String(raw).replace(/\.jpg$/, ''))
  
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const path = await ensureVideoThumb(id)
  setHeader(event, 'content-type', 'image/jpeg')
  if (!path) {
    return PLACEHOLDER
  }
  const stat = statSync(path)
  setHeader(event, 'content-length', String(stat.size))
  setHeader(event, 'cache-control', 'public, max-age=3600')
  return sendStream(event, createReadStream(path))
})
