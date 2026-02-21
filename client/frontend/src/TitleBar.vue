<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { WindowMinimise, WindowToggleMaximise, Quit } from '../wailsjs/runtime/runtime'
import { RenameServer } from './config'
import { BKEN_SCHEME } from './constants'
import { Check, Pencil, Link, Minus, Square, X } from 'lucide-vue-next'
import MetricsBar from './MetricsBar.vue'

const props = defineProps<{ serverName?: string; isOwner?: boolean; serverAddr?: string; voiceConnected?: boolean }>()

const editing = ref(false)
const draft = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
const copied = ref(false)
const showMetricsModal = ref(false)
const COPY_FEEDBACK_MS = 2000

async function copyInvite(): Promise<void> {
  if (!props.serverAddr) return
  const link = `${BKEN_SCHEME}${props.serverAddr}`
  await navigator.clipboard.writeText(link)
  copied.value = true
  setTimeout(() => { copied.value = false }, COPY_FEEDBACK_MS)
}

function startEdit(): void {
  draft.value = props.serverName ?? ''
  editing.value = true
  nextTick(() => inputRef.value?.select())
}

function cancelEdit(): void {
  editing.value = false
}

async function confirmEdit(): Promise<void> {
  const name = draft.value.trim()
  if (name && name !== props.serverName) {
    await RenameServer(name)
  }
  editing.value = false
}

function handleKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter') { e.preventDefault(); confirmEdit() }
  else if (e.key === 'Escape') cancelEdit()
}

// If name changes externally while editing (another client renamed), cancel.
watch(() => props.serverName, () => { editing.value = false })
</script>

<template>
  <div>
  <header
    class="grid grid-cols-[64px_minmax(220px,280px)_minmax(0,1fr)] items-center h-8 shrink-0 bg-base-300 border-b border-base-content/10 select-none"
    style="--wails-draggable: drag"
  >
    <!-- Column 1: App name (over sidebar) -->
    <div
      class="px-2 flex items-center justify-center"
      style="--wails-draggable: no-drag"
    >
      <span class="text-xs font-semibold tracking-widest opacity-40 pointer-events-none">bken</span>
    </div>

    <!-- Column 2: Server name + metrics (over channels list) -->
    <div
      class="flex items-center gap-1.5 px-2 min-w-0"
      style="--wails-draggable: no-drag"
    >
      <template v-if="serverName">
        <!-- Editing: inline input + confirm button -->
        <template v-if="editing">
          <input
            ref="inputRef"
            v-model="draft"
            class="input input-ghost input-xs text-xs h-5 w-36 min-w-0 px-1 py-0 focus:outline-none bg-base-100/30 rounded"
            maxlength="50"
            @keydown="handleKeydown"
            @blur="cancelEdit"
          />
          <!-- mousedown so it fires before blur cancels the edit -->
          <button
            class="btn btn-ghost btn-xs p-0 w-5 h-5 text-success opacity-70 hover:opacity-100 transition-opacity"
            title="Save name"
            tabindex="-1"
            @mousedown.prevent="confirmEdit"
          >
            <Check class="w-3 h-3" aria-hidden="true" />
          </button>
        </template>

        <!-- Display: server name + action icons on hover (owner only) -->
        <template v-else>
          <div class="group/name flex items-center gap-1 min-w-0">
            <span class="text-xs opacity-60 truncate max-w-[120px] pointer-events-none">{{ serverName }}</span>
            <!-- Rename server (pencil) -->
            <button
              v-if="isOwner"
              class="btn btn-ghost btn-xs p-0 w-4 h-4 opacity-0 group-hover/name:opacity-50 hover:!opacity-100 transition-opacity shrink-0"
              title="Rename server"
              @click="startEdit"
            >
              <Pencil class="w-3 h-3" aria-hidden="true" />
            </button>
            <!-- Copy invite link (chain link / check) -->
            <button
              v-if="isOwner && serverAddr"
              class="btn btn-ghost btn-xs p-0 w-4 h-4 opacity-0 group-hover/name:opacity-50 hover:!opacity-100 transition-opacity shrink-0"
              :class="{ 'opacity-100 text-success': copied }"
              :title="copied ? 'Copied!' : 'Copy invite link'"
              @click="copyInvite"
            >
              <Check v-if="copied" class="w-3 h-3" aria-hidden="true" />
              <Link v-else class="w-3 h-3" aria-hidden="true" />
            </button>
          </div>
        </template>
      </template>

      <!-- Metrics pushed to right side of column 2 -->
      <div class="flex-1" />
      <MetricsBar
        v-if="voiceConnected"
        mode="compact"
        class="shrink-0"
        @click="showMetricsModal = true"
      />
    </div>

    <!-- Column 3: Window controls (right-aligned, over chat area) -->
    <div class="join flex justify-end" style="--wails-draggable: no-drag">
      <button
        class="btn btn-ghost btn-sm btn-square join-item opacity-50 hover:opacity-100"
        aria-label="Minimise window"
        @click="WindowMinimise()"
      >
        <Minus class="w-2.5 h-2.5" aria-hidden="true" />
      </button>
      <button
        class="btn btn-ghost btn-sm btn-square join-item opacity-50 hover:opacity-100"
        aria-label="Maximise window"
        @click="WindowToggleMaximise()"
      >
        <Square class="w-2.5 h-2.5" aria-hidden="true" />
      </button>
      <button
        class="btn btn-ghost btn-sm btn-square join-item opacity-50 hover:opacity-100 hover:btn-error"
        aria-label="Close window"
        @click="Quit()"
      >
        <X class="w-2.5 h-2.5" aria-hidden="true" />
      </button>
    </div>
  </header>

  <!-- Connection details modal -->
  <dialog class="modal" :class="{ 'modal-open': showMetricsModal }">
    <div class="modal-box w-80">
      <h3 class="text-sm font-semibold mb-3">Connection Details</h3>
      <MetricsBar mode="expanded" />
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" @click="showMetricsModal = false">Close</button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop" @click="showMetricsModal = false">
      <button>close</button>
    </form>
  </dialog>
  </div>
</template>
