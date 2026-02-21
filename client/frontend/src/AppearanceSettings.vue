<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { GetConfig, SaveConfig } from './config'
import type { MessageDensity } from './config'
import ThemePicker from './ThemePicker.vue'
import { AlignLeft, Settings } from 'lucide-vue-next'

const density = ref<MessageDensity>('default')
const showSystemMessages = ref(true)

const densityOptions: { value: MessageDensity; label: string; desc: string }[] = [
  { value: 'compact', label: 'Compact', desc: 'No avatars, inline names, minimal padding' },
  { value: 'default', label: 'Default', desc: 'Small avatar, name above message' },
  { value: 'comfortable', label: 'Comfortable', desc: 'Larger avatar, more spacing' },
]

onMounted(async () => {
  const cfg = await GetConfig()
  density.value = cfg.message_density ?? 'default'
  showSystemMessages.value = cfg.show_system_messages ?? true
})

async function setDensity(d: MessageDensity): Promise<void> {
  density.value = d
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, message_density: d })
  window.dispatchEvent(new CustomEvent('density-changed', { detail: d }))
}

async function toggleSystemMessages(): Promise<void> {
  showSystemMessages.value = !showSystemMessages.value
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, show_system_messages: showSystemMessages.value })
  window.dispatchEvent(new CustomEvent('system-messages-changed', { detail: showSystemMessages.value }))
}
</script>

<template>
  <ThemePicker />

  <section>
    <div class="flex items-center gap-2 mb-3">
      <AlignLeft class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Message Density</span>
    </div>

    <div class="space-y-1.5">
      <button
        v-for="opt in densityOptions"
        :key="opt.value"
        class="w-full rounded-lg px-3 py-2 text-left text-xs transition-all cursor-pointer border"
        :class="density === opt.value
          ? 'border-primary bg-primary/10 shadow-sm'
          : 'border-base-content/10 hover:border-primary/40 hover:bg-base-200/60'"
        @click="setDensity(opt.value)"
      >
        <span class="font-medium">{{ opt.label }}</span>
        <span class="opacity-50 ml-2">{{ opt.desc }}</span>
      </button>
    </div>
  </section>

  <section>
    <div class="flex items-center gap-2 mb-3">
      <Settings class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Chat</span>
    </div>

    <label class="flex items-center gap-2 cursor-pointer">
      <input
        type="checkbox"
        class="toggle toggle-sm toggle-primary"
        :checked="showSystemMessages"
        @change="toggleSystemMessages"
      />
      <span class="text-xs">Show system messages</span>
    </label>
  </section>
</template>
