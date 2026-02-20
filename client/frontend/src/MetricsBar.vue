<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { GetMetrics } from '../wailsjs/go/main/App'

interface QualityMetrics {
  rtt_ms: number
  packet_loss: number
  jitter_ms: number
  bitrate_kbps: number
  opus_target_kbps: number
  quality_level: string
}

const m = ref<QualityMetrics>({
  rtt_ms: 0,
  packet_loss: 0,
  jitter_ms: 0,
  bitrate_kbps: 0,
  opus_target_kbps: 0,
  quality_level: '',
})

const qualityDot = computed(() => {
  switch (m.value.quality_level) {
    case 'good': return { color: 'bg-success', label: 'Good connection' }
    case 'moderate': return { color: 'bg-warning', label: 'Moderate connection' }
    case 'poor': return { color: 'bg-error', label: 'Poor connection' }
    default: return { color: 'bg-base-content/30', label: 'No data' }
  }
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
      }
    }
  }, 5000)
})

onBeforeUnmount(() => {
  EventsOff('voice:quality')
  if (interval !== undefined) clearInterval(interval)
})
</script>

<template>
  <div class="flex items-center gap-2 text-xs font-mono" role="status" aria-label="Connection quality">
    <!-- Quality dot -->
    <span
      class="w-2 h-2 rounded-full shrink-0"
      :class="qualityDot.color"
      :title="qualityDot.label"
    />
    <span class="opacity-50" title="Round-trip latency">
      {{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(0) + 'ms' : '---' }}
    </span>
    <span
      title="Packet loss"
      :class="m.packet_loss > 0.05 ? 'text-error' : 'opacity-50'"
    >
      {{ (m.packet_loss * 100).toFixed(0) }}%
    </span>
    <span class="opacity-50" title="Jitter">
      {{ m.jitter_ms > 0 ? m.jitter_ms.toFixed(0) + 'ms' : '---' }}
    </span>
  </div>
</template>
