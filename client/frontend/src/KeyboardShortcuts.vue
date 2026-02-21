<script setup lang="ts">
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
    <dialog class="modal modal-open" @keydown.escape="emit('close')">
      <div class="modal-box max-w-sm">
        <h3 class="text-sm font-semibold mb-4">Keyboard Shortcuts</h3>
        <table class="table table-xs">
          <tbody>
            <tr v-for="s in shortcuts" :key="s.keys">
              <td class="text-xs opacity-70">{{ s.description }}</td>
              <td class="text-right"><kbd class="kbd kbd-xs">{{ s.keys }}</kbd></td>
            </tr>
          </tbody>
        </table>
        <div class="modal-action">
          <button class="btn btn-sm btn-ghost" @click="emit('close')">Close</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="emit('close')">
        <button>close</button>
      </form>
    </dialog>
  </Teleport>
</template>
