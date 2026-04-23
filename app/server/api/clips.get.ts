// GET /api/clips?game=…&sort=…&min_rating=…
//
// Returns the filtered/sorted list of active candidate clips. Schema lives in
// `server/utils/db.ts`; this handler is just query parsing + SQL assembly.

import { CLIP_SELECT, enrichClipsWithVision, db, type ClipRow } from '~~/server/utils/db'

export const SORT_OPTIONS: Record<string, [string, string]> = {
  llm_rank:      ['c.llm_rank ASC NULLS LAST, c.score DESC',     'Best (LLM rank)'],
  recorded_desc: ['v.recorded_at DESC, c.start_s ASC',           'Newest recording'],
  recorded_asc:  ['v.recorded_at ASC, c.start_s ASC',            'Oldest recording'],
  rating_desc:   ['c.user_rating DESC NULLS LAST, c.score DESC', 'Highest user rating'],
  rating_asc:    ['c.user_rating ASC NULLS LAST, c.score DESC',  'Lowest user rating'],
  score_desc:    ['c.score DESC',                                'Heuristic score'],
}

export const DEFAULT_SORT = 'llm_rank'

export default defineEventHandler((event) => {
  const query = getQuery(event)
  const game = typeof query.game === 'string' && query.game ? query.game : null
  const sort = typeof query.sort === 'string' && query.sort in SORT_OPTIONS ? query.sort : DEFAULT_SORT
  const minRating = Math.max(0, Math.min(5, Number(query.min_rating ?? 0) || 0))

  const wheres: string[] = ['c.clip_path IS NOT NULL', 'COALESCE(c.active, 1) = 1']
  const params: (string | number)[] = []
  if (game) {
    wheres.push('v.game = ?')
    params.push(game)
  }
  if (minRating > 0) {
    wheres.push('COALESCE(c.user_rating, 0) >= ?')
    params.push(minRating)
  }

  const orderClause = SORT_OPTIONS[sort][0]
  const sql = `${CLIP_SELECT} WHERE ${wheres.join(' AND ')} ORDER BY ${orderClause}`
  const rows = db().prepare(sql).all(...params) as ClipRow[]

  return {
    clips: enrichClipsWithVision(rows),
    sort_options: Object.entries(SORT_OPTIONS).map(([key, [, label]]) => ({ key, label })),
    default_sort: DEFAULT_SORT,
  }
})
