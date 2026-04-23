// GET /api/videos
//
// List all indexed videos with processing status and moment counts.

import { db } from '~~/server/utils/db'

export default defineEventHandler((event) => {
  const query = getQuery(event)
  
  const showMissing = query.missing === '1' || query.missing === 'true'

  let sql = `
    SELECT v.*,
           (SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1) AS moment_count,
           (SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1 AND c.clip_path IS NOT NULL) AS clip_count
      FROM video v
     WHERE 1=1
  `

  const params: any[] = []

  // Filters
  if (!showMissing) {
    sql += ` AND v.missing = 0`
  }

  if (query.library_id) {
    sql += ` AND v.library_id = ?`
    params.push(Number(query.library_id))
  }

  if (query.q) {
    sql += ` AND v.path LIKE ?`
    params.push(`%${query.q}%`)
  }

  if (query.game) {
    sql += ` AND v.game = ?`
    params.push(query.game)
  }

  if (query.date_from) {
    sql += ` AND v.recorded_at >= ?`
    params.push(query.date_from)
  }

  if (query.date_to) {
    sql += ` AND v.recorded_at <= ?`
    params.push(query.date_to + 'T23:59:59') // assume date-only format
  }

  if (query.min_moments) {
    sql += ` AND (SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1) >= ?`
    params.push(Number(query.min_moments))
  }

  // Sorting
  const sort = query.sort || 'recorded_at'
  const order = query.order === 'asc' ? 'ASC' : 'DESC'
  
  const allowedSorts = ['recorded_at', 'duration_s', 'moment_count', 'name', 'created_at']
  const actualSort = allowedSorts.includes(sort as string) ? sort : 'recorded_at'
  
  if (actualSort === 'moment_count') {
    sql += ` ORDER BY moment_count ${order}`
  } else if (actualSort === 'name') {
    sql += ` ORDER BY v.path ${order}`
  } else {
    sql += ` ORDER BY v.${actualSort} ${order}`
  }

  const rows = db().prepare(sql).all(...params) as any[]

  return {
    videos: rows.map((r) => ({
      id: r.id,
      path: r.path,
      name: r.path.split('/').pop(),
      game: r.game,
      duration_s: r.duration_s,
      width: r.width,
      height: r.height,
      recorded_at: r.recorded_at,
      missing: !!r.missing,
      library_id: r.library_id,
      status: {
        audio: !!r.audio_done,
        transcribe: !!r.transcribe_done,
        events: !!r.events_done,
        vision: !!r.vision_done,
        score: !!r.score_done,
        rank: !!r.rank_done,
      },
      moment_count: r.moment_count,
      clip_count: r.clip_count,
      thumb_url: `/video_thumb/${r.id}.jpg`,
      created_at: r.created_at,
      updated_at: r.updated_at,
    })),
  }
})
