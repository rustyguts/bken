<script setup lang="ts">
import AudioSettings from './AudioSettings.vue'

const emit = defineEmits<{
  back: []
}>()
</script>

<template>
  <section class="flex h-full min-h-0 flex-col bg-base-100">
    <header class="flex items-center gap-2 px-3 sm:px-5 py-3 border-b border-base-content/10 shrink-0">
      <button class="btn btn-ghost btn-sm" @click="emit('back')" aria-label="Back to room">
        ← Back
      </button>
      <h2 class="text-sm font-semibold">Settings</h2>
    </header>

    <AudioSettings class="flex-1 min-h-0" :show-header="false" />
  </section>
</template>
