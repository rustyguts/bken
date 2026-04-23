import { ref, onMounted, onUnmounted, watch } from 'vue'

export interface VideoItem {
  id: number
  name: string
  game: string | null
  duration_s: number | null
  width: number | null
  height: number | null
  thumb_url: string
  recorded_at: string | null
  missing: boolean
  library_id: number | null
  status: {
    audio: boolean
    transcribe: boolean
    events: boolean
    vision: boolean
    score: boolean
    rank: boolean
  }
  moment_count: number
  clip_count: number
  created_at: string
  updated_at: string
}

export interface LibraryItem {
  id: number
  name: string
  path: string
  scan_interval_minutes: number
  last_scan_at: string | null
  next_scan_at: string | null
  active: boolean
  video_count: number
  missing_count: number
  created_at: string
}

export interface JobItem {
  id: number
  video_id: number | null
  video_name: string | null
  game: string | null
  type: string
  status: string
  target_id: number | null
  error_message: string | null
  created_at: string
  updated_at: string
}

export function useDashboard() {
  const videos = ref<VideoItem[]>([])
  const jobs = ref<JobItem[]>([])
  const loading = ref(false)
  const error = ref('')
  let pollTimer: ReturnType<typeof setInterval> | null = null

  const filters = ref({
    q: '',
    game: '',
    min_moments: '',
    date_from: '',
    date_to: '',
    sort: 'recorded_at',
    order: 'desc',
    library_id: '',
    missing: '0',
  })

  async function fetchVideos() {
    try {
      const params = new URLSearchParams()
      if (filters.value.q) params.append('q', filters.value.q)
      if (filters.value.game) params.append('game', filters.value.game)
      if (filters.value.min_moments) params.append('min_moments', filters.value.min_moments)
      if (filters.value.date_from) params.append('date_from', filters.value.date_from)
      if (filters.value.date_to) params.append('date_to', filters.value.date_to)
      if (filters.value.sort) params.append('sort', filters.value.sort)
      if (filters.value.order) params.append('order', filters.value.order)
      if (filters.value.library_id) params.append('library_id', filters.value.library_id)
      if (filters.value.missing === '1') params.append('missing', '1')

      const res = await fetch(`/api/videos?${params.toString()}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      videos.value = data.videos
    } catch (e: any) {
      error.value = e.message
    }
  }

  async function fetchLibraries(): Promise<LibraryItem[]> {
    try {
      const res = await fetch('/api/libraries')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      return data.libraries
    } catch {
      return []
    }
  }

  async function fetchJobs() {
    try {
      const res = await fetch('/api/jobs')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      jobs.value = data.jobs
    } catch (e: any) {
      // non-fatal
    }
  }

  async function reload() {
    loading.value = true
    await Promise.all([fetchVideos(), fetchJobs()])
    loading.value = false
  }

  watch(filters, () => {
    fetchVideos()
  }, { deep: true })

  async function reprocessVideo(id: number, fromStage: string = 'score') {
    await fetch(`/api/videos/${id}/reprocess`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from_stage: fromStage }),
    })
    await fetchJobs()
  }

  function startPolling() {
    pollTimer = setInterval(fetchJobs, 3000)
  }

  function stopPolling() {
    if (pollTimer) clearInterval(pollTimer)
  }

  onMounted(() => {
    reload()
    startPolling()
  })

  onUnmounted(stopPolling)

  return {
    videos,
    jobs,
    loading,
    error,
    filters,
    reload,
    reprocessVideo,
    fetchLibraries,
  }
}
