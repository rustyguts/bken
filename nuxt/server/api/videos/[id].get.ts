// GET /api/videos/:id
//
// Full metadata for one video.

import { db } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const row = db()
    .prepare('SELECT * FROM video WHERE id = ?')
    .get(id) as any | undefined

  if (!row) {
    throw createError({ statusCode: 404, statusMessage: 'video not found' })
  }

  return {
    id: row.id,
    path: row.path,
    name: row.path.split('/').pop(),
    game: row.game,
    duration_s: row.duration_s,
    width: row.width,
    height: row.height,
    fps: row.fps,
    video_codec: row.video_codec,
    audio_codec: row.audio_codec,
    recorded_at: row.recorded_at,
    status: {
      audio: !!row.audio_done,
      transcribe: !!row.transcribe_done,
      events: !!row.events_done,
      vision: !!row.vision_done,
      score: !!row.score_done,
      rank: !!row.rank_done,
    },
    created_at: row.created_at,
    updated_at: row.updated_at,
  }
})
