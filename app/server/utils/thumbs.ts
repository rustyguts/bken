// Lazy thumbnail rendering — mirrors src/bken/thumbs.py line-for-line.
//
// ffmpeg is spawned as a subprocess; the output is cached to
// data/thumbs/clip_<id>.jpg. Colour filter undoes the NTSC-era tags the
// recorder stamps onto 1080p footage; without it JPEGs looked washed out.

import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, statSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { db } from './db'
import { DATA_DIR, THUMBS_DIR, resolveData } from './paths'
// DATA_DIR kept in scope for the relative() call below.

const THUMB_WIDTH = 640

export function thumbPathFor(clipId: number): string {
  return resolve(THUMBS_DIR, `clip_${String(clipId).padStart(6, '0')}.jpg`)
}

/**
 * Return an absolute path to the thumbnail, rendering it on first request.
 * Null if the clip has no mp4 on disk yet.
 */
export async function ensureThumb(clipId: number): Promise<string | null> {
  const out = thumbPathFor(clipId)
  if (existsSync(out) && statSync(out).size > 0) return out

  const row = db()
    .prepare('SELECT clip_path, start_s, end_s FROM candidate_clip WHERE id=?')
    .get(clipId) as { clip_path: string | null; start_s: number; end_s: number } | undefined
  if (!row || !row.clip_path) return null

  const clipFile = resolveData(row.clip_path)
  if (!existsSync(clipFile)) return null

  const dur = Math.max(0.5, row.end_s - row.start_s)
  const seek = Math.min(dur / 2, dur - 0.1)

  mkdirSync(dirname(out), { recursive: true })

  // Colour pipeline: source is mis-tagged bt470m/smpte170m but actually
  // 1080p bt709. `setparams` fixes the label; `scale`'s matrix conversion
  // then re-samples YUV into JPEG's BT.601 decode assumption. Same filter
  // chain as bken/thumbs.py on the Python side.
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
      clipFile,
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

  // Store the thumbnail path relative to DATA_DIR so it travels with the DB.
  const rel = relative(DATA_DIR, out)
  db().prepare('UPDATE candidate_clip SET thumb_path=? WHERE id=?').run(rel, clipId)
  return out
}
