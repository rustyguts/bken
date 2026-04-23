// GET /api/games
//
// Sidebar roll-up. One row per game with clip count and average rating.

import { db } from '~~/server/utils/db'

export default defineEventHandler(() => {
  const rows = db()
    .prepare(
      `SELECT v.game AS game,
              COUNT(c.id) AS n,
              AVG(c.user_rating) AS avg_rating,
              SUM(CASE WHEN c.user_rating IS NOT NULL THEN 1 ELSE 0 END) AS rated
         FROM video v
         LEFT JOIN candidate_clip c
                ON c.video_id = v.id
               AND c.clip_path IS NOT NULL
               AND COALESCE(c.active, 1) = 1
        WHERE v.game IS NOT NULL
        GROUP BY v.game
        ORDER BY n DESC, v.game ASC`,
    )
    .all()
  return { games: rows }
})
