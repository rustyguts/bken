<script setup lang="ts">
import { ref, nextTick } from 'vue'
import MetricsBar from './MetricsBar.vue'
import { Mic, MicOff, Volume2, VolumeX, Video, Monitor, Settings, LogOut } from 'lucide-vue-next'

const props = defineProps<{
  username: string
  muted: boolean
  deafened: boolean
  connected: boolean
  voiceConnected: boolean
  videoActive: boolean
  screenSharing: boolean
}>()

const emit = defineEmits<{
  'rename-username': [username: string]
  'open-settings': []
  'mute-toggle': []
  'deafen-toggle': []
  'leave-voice': []
  'video-toggle': []
  'screen-share-toggle': []
}>()

const modalOpen = ref(false)
const modalInput = ref('')
const modalInputEl = ref<HTMLInputElement | null>(null)

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
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
        <Mic v-if="!muted" class="w-4 h-4" aria-hidden="true" />
        <MicOff v-else class="w-4 h-4" aria-hidden="true" />
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
        <Volume2 v-if="!deafened" class="w-4 h-4" aria-hidden="true" />
        <VolumeX v-else class="w-4 h-4" aria-hidden="true" />
      </button>

      <!-- Video -->
      <button
        class="btn btn-ghost btn-sm"
        :class="videoActive ? 'text-success' : ''"
        :disabled="!voiceConnected"
        :title="videoActive ? 'Stop Video' : 'Start Video'"
        @click="emit('video-toggle')"
      >
        <Video class="w-4 h-4" aria-hidden="true" />
      </button>

      <!-- Screen Share -->
      <button
        class="btn btn-ghost btn-sm"
        :class="screenSharing ? 'text-success' : ''"
        :disabled="!voiceConnected"
        :title="screenSharing ? 'Stop Sharing' : 'Share Screen'"
        @click="emit('screen-share-toggle')"
      >
        <Monitor class="w-4 h-4" aria-hidden="true" />
      </button>

      <!-- Settings -->
      <button
        class="btn btn-ghost btn-sm"
        title="Open Settings"
        @click="emit('open-settings')"
      >
        <Settings class="w-4 h-4" aria-hidden="true" />
      </button>

      <!-- Leave Voice Channel -->
      <button
        class="btn btn-error btn-sm btn-ghost"
        :disabled="!voiceConnected"
        title="Leave Voice Channel"
        @click="emit('leave-voice')"
      >
        <LogOut class="w-4 h-4" aria-hidden="true" />
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
          <button class="btn btn-soft btn-primary" :disabled="!modalInput.trim()" @click="confirmRename">Save</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="modalOpen = false">
        <button>close</button>
      </form>
    </dialog>
  </div>
</template>
