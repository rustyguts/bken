// GET /api/videos/:id/moments
//
// All moments (candidate clips) for a video, including manual ones.

import { db } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw)
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid video id' })
  }

  const rows = db()
    .prepare(
      `SELECT * FROM candidate_clip
        WHERE video_id = ? AND active = 1
        ORDER BY start_s ASC`,
    )
    .all(id) as any[]

  return {
    moments: rows.map((r) => ({
      id: r.id,
      video_id: r.video_id,
      start_s: r.start_s,
      end_s: r.end_s,
      duration: r.end_s - r.start_s,
      score: r.score,
      title: r.title || r.llm_title || `Moment @ ${Math.floor(r.start_s / 60)}:${String(Math.floor(r.start_s % 60)).padStart(2, '0')}`,
      llm_rank: r.llm_rank,
      llm_title: r.llm_title,
      llm_desc: r.llm_desc,
      llm_contents: r.llm_contents,
      llm_tags: r.llm_tags,
      transcript: r.transcript,
      clip_path: r.clip_path,
      clip_stale: Boolean(r.clip_stale),
      thumb_path: r.thumb_path,
      user_rating: r.user_rating,
      source: r.source,
      features: r.features,
    })),
  }
})
