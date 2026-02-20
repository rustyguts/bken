<script setup lang="ts">
import { onMounted } from 'vue'
import { useTheme } from './composables/useTheme'

const { THEMES, currentTheme, applyTheme, restoreFromConfig } = useTheme()

onMounted(() => {
  restoreFromConfig()
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4.098 19.902a3.75 3.75 0 005.304 0l6.401-6.402M6.75 21A3.75 3.75 0 013 17.25V4.125C3 3.504 3.504 3 4.125 3h5.25c.621 0 1.125.504 1.125 1.125v4.072M6.75 21a3.75 3.75 0 003.75-3.75V8.197M6.75 21h13.125c.621 0 1.125-.504 1.125-1.125v-5.25c0-.621-.504-1.125-1.125-1.125h-4.072M10.5 8.197l2.88-2.88c.438-.439 1.15-.439 1.59 0l3.712 3.713c.44.44.44 1.152 0 1.59l-2.879 2.88M6.75 17.25h.008v.008H6.75v-.008z" />
      </svg>
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Appearance</span>
    </div>

    <div class="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Theme selection">
      <button
        v-for="theme in THEMES"
        :key="theme.name"
        class="rounded-xl px-3 py-2 text-left text-xs transition-all cursor-pointer border truncate"
        :class="currentTheme === theme.name
          ? 'border-primary bg-primary/10 shadow-sm'
          : 'border-base-content/10 hover:border-primary/40 hover:bg-base-200/60'"
        role="radio"
        :aria-checked="currentTheme === theme.name"
        :aria-label="`${theme.label} theme`"
        @click="applyTheme(theme.name)"
      >{{ theme.label }}</button>
    </div>
  </section>
</template>
