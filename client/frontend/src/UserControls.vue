<script setup lang="ts">
import { ref, nextTick } from 'vue'
import MetricsBar from './MetricsBar.vue'

const props = defineProps<{
  username: string
  muted: boolean
  deafened: boolean
  connected: boolean
  voiceConnected: boolean
}>()

const emit = defineEmits<{
  'rename-username': [username: string]
  'open-settings': []
  'mute-toggle': []
  'deafen-toggle': []
  disconnect: []
}>()

const modalOpen = ref(false)
const modalInput = ref('')
const modalInputEl = ref<HTMLInputElement | null>(null)

function initials(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return '??'
  const parts = trimmed.split(/\s+/).filter(Boolean)
  const a = parts[0]?.[0] ?? ''
  const b = parts[1]?.[0] ?? parts[0]?.[1] ?? ''
  return `${a}${b}`.toUpperCase()
}

async function openRenameModal(event: MouseEvent): Promise<void> {
  event.preventDefault()
  modalInput.value = props.username?.trim() ?? ''
  modalOpen.value = true
  await nextTick()
  modalInputEl.value?.focus()
  modalInputEl.value?.select()
}

function confirmRename(): void {
  const cleaned = modalInput.value.trim()
  if (!cleaned || cleaned === (props.username?.trim() ?? '')) {
    modalOpen.value = false
    return
  }
  emit('rename-username', cleaned)
  modalOpen.value = false
}
</script>

<template>
  <div class="border-t border-base-content/10 bg-base-200 p-2 flex flex-col gap-1">
    <div class="flex items-center gap-1">
      <!-- Avatar (right-click to rename) -->
      <button
        class="w-8 h-8 rounded-full bg-base-300 border border-base-content/20 text-xs font-mono flex items-center justify-center shrink-0 hover:ring-2 hover:ring-primary/40 transition-shadow cursor-pointer"
        :title="username ? `${username} (right-click to rename)` : 'Right-click to set username'"
        @contextmenu="openRenameModal"
      >
        {{ initials(username) }}
      </button>

      <!-- Mute -->
      <button
        class="btn btn-ghost btn-sm"
        :class="muted ? 'text-error' : ''"
        :aria-pressed="muted"
        :disabled="!voiceConnected"
        :title="muted ? 'Unmute' : 'Mute'"
        @click="emit('mute-toggle')"
      >
        <svg v-if="!muted" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
        </svg>
        <svg v-else xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
          <path stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18" />
        </svg>
      </button>

      <!-- Deafen -->
      <button
        class="btn btn-ghost btn-sm"
        :class="deafened ? 'text-error' : ''"
        :aria-pressed="deafened"
        :disabled="!voiceConnected"
        :title="deafened ? 'Undeafen' : 'Deafen'"
        @click="emit('deafen-toggle')"
      >
        <svg v-if="!deafened" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M14.463 10.29a3.75 3.75 0 010 3.42M9.537 13.71a3.75 3.75 0 010-3.42M4.886 18.364a9 9 0 010-12.728M12 12h.008v.008H12V12z" />
        </svg>
        <svg v-else xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M14.463 10.29a3.75 3.75 0 010 3.42M9.537 13.71a3.75 3.75 0 010-3.42M4.886 18.364a9 9 0 010-12.728M12 12h.008v.008H12V12z" />
          <path stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18" />
        </svg>
      </button>

      <!-- Settings -->
      <button
        class="btn btn-ghost btn-sm"
        title="Open Settings"
        @click="emit('open-settings')"
      >
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 010 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 010-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28z" />
          <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
      </button>

      <!-- Disconnect -->
      <button
        class="btn btn-ghost btn-sm hover:text-error"
        :disabled="!voiceConnected"
        title="Leave Voice"
        @click="emit('disconnect')"
      >
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9" />
        </svg>
      </button>
    </div>

    <MetricsBar v-if="voiceConnected" />

    <!-- DaisyUI modal for username rename -->
    <dialog class="modal" :class="{ 'modal-open': modalOpen }">
      <div class="modal-box w-80">
        <h3 class="text-lg font-bold">Set Username</h3>
        <div class="py-4">
          <input
            ref="modalInputEl"
            v-model="modalInput"
            type="text"
            placeholder="Enter username"
            class="input input-bordered w-full"
            maxlength="32"
            @keydown.enter.prevent="confirmRename"
          />
        </div>
        <div class="modal-action">
          <button class="btn btn-ghost" @click="modalOpen = false">Cancel</button>
          <button class="btn btn-primary" :disabled="!modalInput.trim()" @click="confirmRename">Save</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="modalOpen = false">
        <button>close</button>
      </form>
    </dialog>
  </div>
</template>
