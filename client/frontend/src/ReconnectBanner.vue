<script setup lang="ts">
import { AlertTriangle } from 'lucide-vue-next'

defineProps<{
  attempt: number
  secondsUntilRetry: number
  reason: string
}>()

const emit = defineEmits<{ cancel: [] }>()
</script>

<template>
  <div
    class="fixed top-0 left-0 right-0 z-50 flex items-center justify-between gap-4 px-4 py-2.5 bg-warning text-warning-content text-sm shadow-md"
    role="alert"
    aria-live="assertive"
  >
    <div class="flex items-center gap-2">
      <AlertTriangle class="w-4 h-4 shrink-0" aria-hidden="true" />
      <span>
        {{ reason || 'Connection lost' }} ---
        <span v-if="secondsUntilRetry > 0">retrying in {{ secondsUntilRetry }}s</span>
        <span v-else>reconnecting{{ attempt > 0 ? ` (attempt ${attempt})` : '' }}...</span>
      </span>
    </div>
    <button
      class="btn btn-xs btn-ghost font-normal"
      aria-label="Cancel reconnection"
      @click="emit('cancel')"
    >
      Cancel
    </button>
  </div>
</template>
