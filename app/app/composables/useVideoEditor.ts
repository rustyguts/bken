import { ref, computed } from 'vue'

export interface Moment {
  id: number
  video_id: number
  start_s: number
  end_s: number
  duration: number
  score: number
  title: string
  llm_rank: number | null
  llm_title: string | null
  llm_desc: string | null
  llm_contents: string | null
  llm_tags: string | null
  transcript: string | null
  clip_path: string | null
  clip_stale: boolean
  thumb_path: string | null
  user_rating: number | null
  source: string
  features: string | null
}

export interface VideoDetail {
  id: number
  name: string
  game: string | null
  duration_s: number | null
  width: number | null
  height: number | null
  status: {
    audio: boolean
    transcribe: boolean
    events: boolean
    vision: boolean
    score: boolean
    rank: boolean
  }
}

export type SortField =
  | 'id'
  | 'video_id'
  | 'start_s'
  | 'end_s'
  | 'duration'
  | 'score'
  | 'llm_rank'
  | 'user_rating'
  | 'title'
  | 'llm_title'
  | 'llm_desc'
  | 'llm_contents'
  | 'llm_tags'
  | 'transcript'
  | 'clip_path'
  | 'clip_stale'
  | 'thumb_path'
  | 'source'
  | 'features'

export const SORT_OPTIONS: { label: string; value: SortField }[] = [
  { label: 'Start time', value: 'start_s' },
  { label: 'End time', value: 'end_s' },
  { label: 'Duration', value: 'duration' },
  { label: 'Score', value: 'score' },
  { label: 'LLM rank', value: 'llm_rank' },
  { label: 'User rating', value: 'user_rating' },
  { label: 'ID', value: 'id' },
  { label: 'Video ID', value: 'video_id' },
  { label: 'Title', value: 'title' },
  { label: 'LLM title', value: 'llm_title' },
  { label: 'Description', value: 'llm_desc' },
  { label: 'Contents', value: 'llm_contents' },
  { label: 'Tags', value: 'llm_tags' },
  { label: 'Transcript', value: 'transcript' },
  { label: 'Clip path', value: 'clip_path' },
  { label: 'Clip stale', value: 'clip_stale' },
  { label: 'Thumb path', value: 'thumb_path' },
  { label: 'Source', value: 'source' },
  { label: 'Features', value: 'features' },
]

export function useVideoEditor(videoId: number) {
  const video = ref<VideoDetail | null>(null)
  const moments = ref<Moment[]>([])
  const loading = ref(false)
  const error = ref('')
  const currentTime = ref(0)
  const selectedMomentId = ref<number | null>(null)

  const sortField = ref<SortField>('start_s')
  const sortDirection = ref<'asc' | 'desc'>('asc')

  const selectedMoment = computed(() =>
    moments.value.find((m) => m.id === selectedMomentId.value) || null
  )

  const sortedMoments = computed(() => {
    const field = sortField.value
    const dir = sortDirection.value === 'asc' ? 1 : -1
    return [...moments.value].sort((a, b) => {
      const aVal = (a as any)[field]
      const bVal = (b as any)[field]
      if (aVal == null && bVal == null) return 0
      if (aVal == null) return 1 * dir
      if (bVal == null) return -1 * dir
      if (typeof aVal === 'string' && typeof bVal === 'string') {
        return aVal.localeCompare(bVal) * dir
      }
      if (typeof aVal === 'number' && typeof bVal === 'number') {
        return (aVal - bVal) * dir
      }
      if (typeof aVal === 'boolean' && typeof bVal === 'boolean') {
        return (Number(aVal) - Number(bVal)) * dir
      }
      return String(aVal).localeCompare(String(bVal)) * dir
    })
  })

  async function fetchVideo() {
    const res = await fetch(`/api/videos/${videoId}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    video.value = await res.json()
  }

  async function fetchMoments() {
    const res = await fetch(`/api/videos/${videoId}/moments`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    moments.value = data.moments
  }

  async function reload() {
    loading.value = true
    try {
      await Promise.all([fetchVideo(), fetchMoments()])
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function updateMoment(id: number, patch: Partial<Pick<Moment, 'title' | 'start_s' | 'end_s'>>) {
    await fetch(`/api/moments/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    await fetchMoments()
  }

  async function deleteMoment(id: number) {
    await fetch(`/api/moments/${id}`, { method: 'DELETE' })
    if (selectedMomentId.value === id) selectedMomentId.value = null
    await fetchMoments()
  }

  async function createClip(id: number) {
    await fetch(`/api/moments/${id}/clip`, { method: 'POST' })
    await fetchMoments()
  }

  async function createMoment(startS: number, endS: number, title: string) {
    const res = await fetch('/api/moments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ video_id: videoId, start_s: startS, end_s: endS, title }),
    })
    if (!res.ok) throw new Error('Failed to create moment')
    await fetchMoments()
  }

  function seekTo(time: number) {
    currentTime.value = time
  }

  function selectMoment(id: number) {
    selectedMomentId.value = id
    const m = moments.value.find((x) => x.id === id)
    if (m) currentTime.value = m.start_s
  }

  return {
    video,
    moments,
    sortedMoments,
    loading,
    error,
    currentTime,
    selectedMomentId,
    selectedMoment,
    sortField,
    sortDirection,
    reload,
    updateMoment,
    deleteMoment,
    createClip,
    createMoment,
    seekTo,
    selectMoment,
  }
}
