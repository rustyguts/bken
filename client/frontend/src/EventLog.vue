<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import type { LogEvent } from './types'

const props = defineProps<{ events: LogEvent[] }>()

const scrollEl = ref<HTMLElement>()

watch(() => props.events.length, async () => {
  await nextTick()
  if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight
})
</script>

<template>
  <div class="flex flex-col min-h-0 h-full overflow-hidden">
    <div class="px-3 py-2 text-xs font-semibold uppercase tracking-wider opacity-40 border-b border-base-content/10 shrink-0">
      Events
    </div>
    <div ref="scrollEl" class="flex-1 overflow-y-auto p-2 flex flex-col gap-0.5" role="log" aria-label="Event log" aria-live="polite">
      <div v-if="events.length === 0" class="text-xs opacity-25 italic px-1 pt-1">No events yet</div>
      <div v-for="ev in events" :key="ev.id" class="flex gap-1.5 text-xs leading-5">
        <span class="opacity-30 font-mono shrink-0 tabular-nums">{{ ev.time }}</span>
        <span :class="{
          'text-success': ev.type === 'join',
          'text-error': ev.type === 'leave',
          'opacity-60': ev.type === 'info',
        }">{{ ev.text }}</span>
      </div>
    </div>
  </div>
</template>
