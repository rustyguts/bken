// GET /api/search/ocr?q=wasted
//
// Returns clip IDs whose vision_ocr.text_upper LIKE '%WASTED%',
// joined to candidate_clip on time overlap.

import { CLIP_SELECT, enrichClipsWithVision, db, type ClipRow } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const query = getQuery(event)
  const q = typeof query.q === 'string' && query.q ? query.q.toUpperCase() : ''
  if (!q) {
    return { clips: [] }
  }

  const sql = `${CLIP_SELECT}
    JOIN vision_ocr o ON o.video_id = c.video_id
      AND o.ts_s BETWEEN c.start_s AND c.end_s
    WHERE c.clip_path IS NOT NULL
      AND COALESCE(c.active, 1) = 1
      AND o.text_upper LIKE ?
    GROUP BY c.id
    ORDER BY c.score DESC`

  const rows = db().prepare(sql).all(`%${q}%`) as ClipRow[]
  return { clips: enrichClipsWithVision(rows) }
})
