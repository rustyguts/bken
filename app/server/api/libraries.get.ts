// GET /api/libraries
//
// List all libraries with video counts.

import { db } from '~~/server/utils/db'

export default defineEventHandler(() => {
  const rows = db().prepare(`
    SELECT l.*,
           COUNT(v.id) AS video_count,
           SUM(CASE WHEN v.missing = 1 THEN 1 ELSE 0 END) AS missing_count
    FROM library l
    LEFT JOIN video v ON v.library_id = l.id
    GROUP BY l.id
    ORDER BY l.name
  `).all() as any[]

  return {
    libraries: rows.map((r) => ({
      id: r.id,
      name: r.name,
      path: r.path,
      scan_interval_minutes: r.scan_interval_minutes,
      last_scan_at: r.last_scan_at,
      next_scan_at: r.next_scan_at,
      active: !!r.active,
      video_count: r.video_count,
      missing_count: r.missing_count,
      created_at: r.created_at,
    })),
  }
})
