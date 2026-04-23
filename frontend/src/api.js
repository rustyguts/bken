// Thin wrapper around the FastAPI JSON endpoints.
//
// Keeping the fetch logic in one place makes it easy to swap base URLs
// (e.g. during dev, when Vite proxies /api to the Python server) and to
// add auth headers later.

async function jget(url) {
  const r = await fetch(url, { credentials: 'same-origin' })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

async function jpost(url) {
  const r = await fetch(url, { method: 'POST', credentials: 'same-origin' })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

export function fetchClips({ game = null, sort = 'llm_rank', min_rating = 0 } = {}) {
  const p = new URLSearchParams()
  if (game) p.set('game', game)
  if (sort) p.set('sort', sort)
  if (min_rating) p.set('min_rating', String(min_rating))
  return jget(`/api/clips?${p}`)
}

export function fetchGames() {
  return jget('/api/games')
}

export function rateClip(id, rating) {
  return jpost(`/api/rate/${id}?rating=${rating}`)
}
