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
        class="rounded-lg px-2.5 py-1.5 text-left text-xs transition-all cursor-pointer border w-full"
        :class="themeMode === 'system'
          ? 'border-primary bg-primary/10 shadow-sm'
          : 'border-base-content/10 hover:border-primary/40 hover:bg-base-200/60'"
        @click="setSystemMode"
      >
        System (follow OS)
      </button>
    </div>

    <div class="grid grid-cols-3 gap-1.5 sm:grid-cols-4 lg:grid-cols-5" role="radiogroup" aria-label="Theme selection">
      <button
        v-for="theme in THEMES"
        :key="theme.name"
        class="rounded-md px-2 py-1.5 text-left text-[11px] leading-tight transition-all cursor-pointer border truncate"
        :class="themeMode !== 'system' && currentTheme === theme.name
          ? 'border-primary bg-primary/10 shadow-sm'
          : 'border-base-content/10 hover:border-primary/40 hover:bg-base-200/60'"
        role="radio"
        :aria-checked="themeMode !== 'system' && currentTheme === theme.name"
        :aria-label="`${theme.label} theme`"
        @click="applyTheme(theme.name)"
      >{{ theme.label }}</button>
    </div>
  </section>
</template>
