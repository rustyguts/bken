<script setup lang="ts">
import { X } from 'lucide-vue-next'

const emit = defineEmits<{
  close: []
}>()

const shortcuts = [
  { keys: 'Ctrl + /', description: 'Show this help' },
  { keys: '?', description: 'Show this help' },
  { keys: 'M', description: 'Toggle mute (outside text input)' },
  { keys: 'D', description: 'Toggle deafen (outside text input)' },
  { keys: 'Ctrl + Shift + M', description: 'Toggle mute (anywhere)' },
  { keys: 'Ctrl + K', description: 'Quick channel switcher' },
  { keys: 'Escape', description: 'Close modals / panels' },
]
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[200] flex items-center justify-center bg-black/40" @click.self="emit('close')" @keydown.escape="emit('close')">
      <div class="bg-base-200 rounded-xl border border-base-content/15 shadow-2xl w-full max-w-sm p-5" @click.stop>
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-sm font-semibold">Keyboard Shortcuts</h2>
          <button class="btn btn-ghost btn-xs btn-square" @click="emit('close')">
            <X class="w-4 h-4" aria-hidden="true" />
          </button>
        </div>

        <div class="space-y-2">
          <div
            v-for="s in shortcuts"
            :key="s.keys"
            class="flex items-center justify-between py-1.5 border-b border-base-content/5 last:border-0"
          >
            <span class="text-xs opacity-70">{{ s.description }}</span>
            <kbd class="kbd kbd-xs font-mono">{{ s.keys }}</kbd>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
