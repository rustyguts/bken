<script setup lang="ts">
defineProps<{ attempt: number; secondsUntilRetry: number }>()
const emit = defineEmits<{ cancel: [] }>()
</script>

<template>
  <div class="fixed top-0 left-0 right-0 z-50 flex items-center justify-between gap-4 px-4 py-2.5 bg-warning text-warning-content text-sm shadow-md">
    <div class="flex items-center gap-2">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-4 h-4 shrink-0">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
      </svg>
      <span>
        Connection lost —
        <span v-if="secondsUntilRetry > 0">retrying in {{ secondsUntilRetry }}s</span>
        <span v-else>reconnecting{{ attempt > 0 ? ` (attempt ${attempt})` : '' }}…</span>
      </span>
    </div>
    <button class="btn btn-xs btn-ghost font-normal" @click="emit('cancel')">Cancel</button>
  </div>
</template>
