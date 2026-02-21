<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, withDefaults } from 'vue'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { GetMetrics } from '../wailsjs/go/main/App'

const METRICS_POLL_MS = 5000
const LOSS_WARN_THRESHOLD = 0.05

interface QualityMetrics {
  rtt_ms: number
  packet_loss: number
  jitter_ms: number
  bitrate_kbps: number
  opus_target_kbps: number
  quality_level: string
  capture_dropped: number
  playback_dropped: number
}

const props = withDefaults(defineProps<{
  mode?: 'compact' | 'expanded'
}>(), { mode: 'compact' })

const m = ref<QualityMetrics>({
  rtt_ms: 0,
  packet_loss: 0,
  jitter_ms: 0,
  bitrate_kbps: 0,
  opus_target_kbps: 0,
  quality_level: '',
  capture_dropped: 0,
  playback_dropped: 0,
})

const totalDrops = computed(() => (m.value.capture_dropped ?? 0) + (m.value.playback_dropped ?? 0))

const qualityDot = computed(() => {
  switch (m.value.quality_level) {
    case 'good': return { color: 'badge-success', label: 'Connected' }
    case 'moderate': return { color: 'badge-warning', label: 'Moderate' }
    case 'poor': return { color: 'badge-error', label: 'Poor' }
    default: return { color: 'badge-ghost', label: 'Connecting' }
  }
})

const codecLabel = computed(() => {
  const target = m.value.opus_target_kbps ?? 0
  if (target > 0) return `Opus ${target}kbps`
  return 'Opus'
})

let interval: ReturnType<typeof setInterval> | undefined

function handleQualityEvent(data: QualityMetrics): void {
  if (data) m.value = data
}

onMounted(() => {
  EventsOn('voice:quality', handleQualityEvent)

  interval = setInterval(async () => {
    const metrics = await GetMetrics()
    if (!m.value.quality_level && metrics) {
      m.value = {
        rtt_ms: metrics.rtt_ms ?? 0,
        packet_loss: metrics.packet_loss ?? 0,
        jitter_ms: metrics.jitter_ms ?? 0,
        bitrate_kbps: metrics.bitrate_kbps ?? 0,
        opus_target_kbps: metrics.opus_target_kbps ?? 0,
        quality_level: metrics.quality_level ?? '',
        capture_dropped: metrics.capture_dropped ?? 0,
        playback_dropped: metrics.playback_dropped ?? 0,
      }
    }
  }, METRICS_POLL_MS)
})

onBeforeUnmount(() => {
  EventsOff('voice:quality')
  if (interval !== undefined) clearInterval(interval)
})
</script>

<template>
  <!-- Compact: badge-based metrics for the titlebar -->
  <div v-if="props.mode === 'compact'" class="flex items-center gap-1 cursor-pointer select-none" role="status" aria-label="Connection quality">
    <span class="badge badge-xs" :class="qualityDot.color">{{ qualityDot.label }}</span>
    <span class="badge badge-xs badge-ghost font-mono" title="Round-trip latency">
      {{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(0) + 'ms' : '---' }}
    </span>
    <span
      class="badge badge-xs font-mono"
      :class="m.packet_loss > LOSS_WARN_THRESHOLD ? 'badge-error' : 'badge-ghost'"
      title="Packet loss"
    >
      {{ (m.packet_loss * 100).toFixed(0) }}%
    </span>
    <span class="badge badge-xs badge-ghost font-mono" title="Codec">{{ codecLabel }}</span>
    <span
      v-if="totalDrops > 0"
      class="badge badge-xs badge-warning font-mono"
      :title="`${m.capture_dropped ?? 0} capture + ${m.playback_dropped ?? 0} playback frames dropped`"
    >
      {{ totalDrops }}d
    </span>
  </div>

  <!-- Expanded: DaisyUI stats for the modal -->
  <div v-else class="stats stats-vertical shadow w-full" role="status" aria-label="Connection details">
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Status</div>
      <div class="stat-value text-sm flex items-center gap-1.5">
        <span class="badge badge-xs" :class="qualityDot.color" />
        <span class="capitalize">{{ m.quality_level || 'unknown' }}</span>
      </div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">RTT</div>
      <div class="stat-value text-sm font-mono">{{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(1) + ' ms' : '---' }}</div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Packet Loss</div>
      <div class="stat-value text-sm font-mono" :class="m.packet_loss > LOSS_WARN_THRESHOLD ? 'text-error' : ''">{{ (m.packet_loss * 100).toFixed(1) }}%</div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Jitter</div>
      <div class="stat-value text-sm font-mono">{{ m.jitter_ms > 0 ? m.jitter_ms.toFixed(1) + ' ms' : '---' }}</div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Bitrate</div>
      <div class="stat-value text-sm font-mono">{{ m.bitrate_kbps > 0 ? m.bitrate_kbps.toFixed(0) + ' kbps' : '---' }}</div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Codec Target</div>
      <div class="stat-value text-sm font-mono">{{ m.opus_target_kbps > 0 ? m.opus_target_kbps + ' kbps' : '---' }}</div>
    </div>
    <div class="stat py-2 px-3">
      <div class="stat-title text-xs">Frames Dropped</div>
      <div class="stat-value text-sm font-mono" :class="totalDrops > 0 ? 'text-warning' : ''">
        {{ m.capture_dropped ?? 0 }} capture / {{ m.playback_dropped ?? 0 }} playback
      </div>
    </div>
  </div>
</template>
