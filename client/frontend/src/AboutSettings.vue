<script setup lang="ts">
import ThemePicker from './ThemePicker.vue'
import { Info } from 'lucide-vue-next'
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <Info class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">About</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4 flex flex-col gap-4">
      <div>
        <p class="text-sm font-medium leading-none">bken</p>
        <p class="text-xs opacity-50 mt-1">LAN voice chat application</p>
      </div>

      <div class="divider my-0 opacity-30"></div>

      <div class="text-xs opacity-60 space-y-1">
        <p>Built with Wails + Vue 3 + Go</p>
        <p>Audio: Opus codec, PortAudio capture</p>
        <p>Transport: WebTransport (QUIC/HTTP3)</p>
      </div>
    </div>
  </section>

  <ThemePicker />
</template>
