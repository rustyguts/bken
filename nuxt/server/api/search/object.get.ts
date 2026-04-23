// GET /api/search/object?label=person&min_count=2
//
// Returns clips where a given object label appears at least min_count times
// within the clip time range.

import { CLIP_SELECT, enrichClipsWithVision, db, type ClipRow } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const query = getQuery(event)
  const label = typeof query.label === 'string' && query.label ? query.label.toLowerCase() : ''
  const minCount = Math.max(1, Number(query.min_count ?? 1) || 1)

  if (!label) {
    return { clips: [] }
  }

  const sql = `${CLIP_SELECT}
    JOIN (
      SELECT c.id AS clip_id, COUNT(*) AS cnt
      FROM candidate_clip c
      JOIN vision_detection d ON d.video_id = c.video_id
        AND d.ts_s BETWEEN c.start_s AND c.end_s
      WHERE c.clip_path IS NOT NULL
        AND COALESCE(c.active, 1) = 1
        AND LOWER(d.label) = ?
      GROUP BY c.id
      HAVING cnt >= ?
    ) matches ON matches.clip_id = c.id
    ORDER BY c.score DESC`

  const rows = db().prepare(sql).all(label, minCount) as ClipRow[]
  return { clips: enrichClipsWithVision(rows) }
})
