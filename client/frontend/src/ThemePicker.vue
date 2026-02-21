<script setup lang="ts">
import { onMounted } from 'vue'
import { useTheme } from './composables/useTheme'
import { Palette } from 'lucide-vue-next'

const { THEMES, currentTheme, themeMode, applyTheme, setSystemMode, restoreFromConfig } = useTheme()

onMounted(() => {
  restoreFromConfig()
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <Palette class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Appearance</span>
    </div>

    <!-- System mode button -->
    <div class="mb-2">
      <button
        class="btn btn-sm w-full"
        :class="themeMode === 'system' ? 'btn-primary' : 'btn-outline'"
        @click="setSystemMode"
      >
        System (follow OS)
      </button>
    </div>

    <div class="grid grid-cols-3 gap-1.5 sm:grid-cols-4 lg:grid-cols-5" role="radiogroup" aria-label="Theme selection">
      <button
        v-for="theme in THEMES"
        :key="theme.name"
        class="btn btn-xs normal-case"
        :class="themeMode !== 'system' && currentTheme === theme.name ? 'btn-primary' : 'btn-ghost btn-outline'"
        role="radio"
        :aria-checked="themeMode !== 'system' && currentTheme === theme.name"
        :aria-label="`${theme.label} theme`"
        @click="applyTheme(theme.name)"
      >{{ theme.label }}</button>
    </div>
  </section>
</template>
