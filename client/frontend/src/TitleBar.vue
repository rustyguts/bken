<script setup lang="ts">
import { WindowMinimise, WindowToggleMaximise, Quit } from '../wailsjs/runtime/runtime'
</script>

<template>
  <header
    class="flex items-center h-8 shrink-0 bg-base-300 border-b border-base-content/10 select-none"
    style="--wails-draggable: drag"
  >
    <!-- App name — draggable -->
    <span class="px-3 text-xs font-semibold tracking-widest opacity-40 pointer-events-none">bken</span>

    <div class="flex-1" />

    <!-- Window controls — not draggable -->
    <div class="flex" style="--wails-draggable: no-drag">
      <!-- Minimise -->
      <button
        class="w-10 h-8 flex items-center justify-center opacity-50 hover:opacity-100 hover:bg-base-content/10 transition-colors"
        aria-label="Minimise window"
        @click="WindowMinimise()"
      >
        <svg width="10" height="1" viewBox="0 0 10 1" fill="currentColor" aria-hidden="true">
          <rect width="10" height="1" />
        </svg>
      </button>

      <!-- Maximise / restore -->
      <button
        class="w-10 h-8 flex items-center justify-center opacity-50 hover:opacity-100 hover:bg-base-content/10 transition-colors"
        aria-label="Maximise window"
        @click="WindowToggleMaximise()"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1" aria-hidden="true">
          <rect x="0.5" y="0.5" width="9" height="9" />
        </svg>
      </button>

      <!-- Close -->
      <button
        class="w-10 h-8 flex items-center justify-center opacity-50 hover:opacity-100 hover:bg-error hover:text-error-content transition-colors"
        aria-label="Close window"
        @click="Quit()"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" aria-hidden="true">
          <line x1="0" y1="0" x2="10" y2="10" />
          <line x1="10" y1="0" x2="0" y2="10" />
        </svg>
      </button>
    </div>
  </header>
</template>
