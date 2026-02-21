<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { GetMetrics } from '../wailsjs/go/main/App'
import { ChevronDown } from 'lucide-vue-next'

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

const expanded = ref(false)
const totalDrops = computed(() => (m.value.capture_dropped ?? 0) + (m.value.playback_dropped ?? 0))

const qualityDot = computed(() => {
  switch (m.value.quality_level) {
    case 'good': return { color: 'bg-success', label: 'Connected' }
    case 'moderate': return { color: 'bg-warning', label: 'Moderate' }
    case 'poor': return { color: 'bg-error', label: 'Poor' }
    default: return { color: 'bg-base-content/30', label: 'Connecting' }
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

  // Fall back to polling in case events aren't flowing yet (e.g. initial render).
  interval = setInterval(async () => {
    const metrics = await GetMetrics()
    // Only use poll data if no pushed data has arrived (quality_level unset).
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
  <div class="text-xs font-mono" role="status" aria-label="Connection quality">
    <!-- Compact bar -->
    <div
      class="flex items-center gap-2 cursor-pointer select-none"
      title="Click to expand connection stats"
      @click="expanded = !expanded"
    >
      <!-- Quality dot + status -->
      <span
        class="w-2 h-2 rounded-full shrink-0"
        :class="qualityDot.color"
        :title="qualityDot.label"
      />
      <span class="opacity-60 text-[10px]">{{ qualityDot.label }}</span>
      <span class="opacity-40">|</span>
      <span class="opacity-50" title="Round-trip latency">
        {{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(0) + 'ms' : '---' }}
      </span>
      <span
        title="Packet loss"
        :class="m.packet_loss > LOSS_WARN_THRESHOLD ? 'text-error' : 'opacity-50'"
      >
        {{ (m.packet_loss * 100).toFixed(0) }}%
      </span>
      <span class="opacity-50" title="Codec">{{ codecLabel }}</span>
      <span
        v-if="totalDrops > 0"
        class="text-warning"
        :title="`${m.capture_dropped ?? 0} capture + ${m.playback_dropped ?? 0} playback frames dropped`"
      >
        {{ totalDrops }}d
      </span>
      <!-- Expand chevron -->
      <ChevronDown class="w-3 h-3 opacity-40 transition-transform ml-auto" :class="expanded ? 'rotate-180' : ''" aria-hidden="true" />
    </div>

    <!-- Expanded stats panel -->
    <Transition name="slide-stats">
      <div v-if="expanded" class="mt-1.5 p-2 bg-base-300 rounded-md border border-base-content/10 space-y-1">
        <div class="flex justify-between">
          <span class="opacity-50">RTT</span>
          <span>{{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(1) + ' ms' : '---' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Packet Loss</span>
          <span :class="m.packet_loss > LOSS_WARN_THRESHOLD ? 'text-error' : ''">{{ (m.packet_loss * 100).toFixed(1) }}%</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Jitter</span>
          <span>{{ m.jitter_ms > 0 ? m.jitter_ms.toFixed(1) + ' ms' : '---' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Bitrate</span>
          <span>{{ m.bitrate_kbps > 0 ? m.bitrate_kbps.toFixed(0) + ' kbps' : '---' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Codec Target</span>
          <span>{{ m.opus_target_kbps > 0 ? m.opus_target_kbps + ' kbps' : '---' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Capture Dropped</span>
          <span :class="(m.capture_dropped ?? 0) > 0 ? 'text-warning' : ''">{{ m.capture_dropped ?? 0 }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Playback Dropped</span>
          <span :class="(m.playback_dropped ?? 0) > 0 ? 'text-warning' : ''">{{ m.playback_dropped ?? 0 }}</span>
        </div>
        <div class="flex justify-between">
          <span class="opacity-50">Quality</span>
          <span class="capitalize">{{ m.quality_level || 'unknown' }}</span>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.slide-stats-enter-active,
.slide-stats-leave-active {
  transition: all 0.15s ease;
}
.slide-stats-enter-from,
.slide-stats-leave-to {
  opacity: 0;
  max-height: 0;
  margin-top: 0;
}
</style>
