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
    class="alert alert-warning fixed top-0 left-0 right-0 z-50 rounded-none shadow-md"
    role="alert"
    aria-live="assertive"
  >
    <AlertTriangle class="w-4 h-4 shrink-0" aria-hidden="true" />
    <span class="text-sm">
      {{ reason || 'Connection lost' }} &mdash;
      <span v-if="secondsUntilRetry > 0">retrying in {{ secondsUntilRetry }}s</span>
      <span v-else>reconnecting{{ attempt > 0 ? ` (attempt ${attempt})` : '' }}...</span>
    </span>
    <button
      class="btn btn-xs btn-ghost font-normal"
      aria-label="Cancel reconnection"
      @click="emit('cancel')"
    >
      Cancel
    </button>
  </div>
</template>
