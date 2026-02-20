<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { GetMetrics } from '../wailsjs/go/main/App'

const m = ref({ rtt_ms: 0, packet_loss: 0, bitrate_kbps: 0 })
let interval: ReturnType<typeof setInterval>

onMounted(() => {
  interval = setInterval(async () => { m.value = await GetMetrics() }, 2000)
})
onBeforeUnmount(() => clearInterval(interval))
</script>

<template>
  <div class="px-3 py-2 border-t border-base-content/10 flex items-center gap-3 text-xs font-mono shrink-0">
    <span class="opacity-50" title="Round-trip latency">
      {{ m.rtt_ms > 0 ? m.rtt_ms.toFixed(0) + 'ms' : '—' }}
    </span>
    <span
      title="Packet loss"
      :class="m.packet_loss > 0.05 ? 'text-error' : 'opacity-50'"
    >
      {{ (m.packet_loss * 100).toFixed(0) }}%
    </span>
    <span class="opacity-50" title="Outgoing bitrate">
      {{ m.bitrate_kbps.toFixed(0) }}kb/s
    </span>
  </div>
</template>
