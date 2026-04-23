// GET /api/jobs
//
// List recent jobs, newest first.

import { db } from '~~/server/utils/db'

export default defineEventHandler(() => {
  // `job` table may not exist yet if the pipeline schema hasn't run a job.
  const tableExists = db()
    .prepare(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='job'`)
    .get()
  if (!tableExists) return { jobs: [] }

  const rows = db()
    .prepare(
      `SELECT j.*, v.path AS video_path, v.game, l.name AS library_name
         FROM job j
         LEFT JOIN video v ON v.id = j.video_id
         LEFT JOIN library l ON l.id = j.library_id
        ORDER BY j.created_at DESC
        LIMIT 100`,
    )
    .all() as any[]

  return {
    jobs: rows.map((r) => ({
      id: r.id,
      video_id: r.video_id,
      library_id: r.library_id,
      video_name: r.video_path ? r.video_path.split('/').pop() : null,
      library_name: r.library_name,
      game: r.game,
      type: r.type,
      status: r.status,
      target_id: r.target_id,
      error_message: r.error_message,
      created_at: r.created_at,
      updated_at: r.updated_at,
    })),
  }
})
