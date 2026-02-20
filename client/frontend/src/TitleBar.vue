<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { WindowMinimise, WindowToggleMaximise, Quit } from '../wailsjs/runtime/runtime'
import { RenameServer } from './config'

const props = defineProps<{ serverName?: string; isOwner?: boolean }>()

const editing = ref(false)
const draft = ref('')
const inputRef = ref<HTMLInputElement | null>(null)

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
  <header
    class="flex items-center h-8 shrink-0 bg-base-300 border-b border-base-content/10 select-none"
    style="--wails-draggable: drag"
  >
    <!-- App name + optional server name -->
    <div
      class="px-3 flex items-center gap-1.5"
      style="--wails-draggable: no-drag"
    >
      <span class="text-xs font-semibold tracking-widest opacity-40 pointer-events-none">bken</span>

      <template v-if="serverName">
        <span class="opacity-20 text-xs pointer-events-none">›</span>

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
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3 h-3" aria-hidden="true">
              <path fill-rule="evenodd" d="M12.416 3.376a.75.75 0 0 1 .208 1.04l-5 7.5a.75.75 0 0 1-1.154.114l-3-3a.75.75 0 0 1 1.06-1.06l2.353 2.353 4.493-6.74a.75.75 0 0 1 1.04-.207Z" clip-rule="evenodd" />
            </svg>
          </button>
        </template>

        <!-- Display: server name + pencil icon on hover (owner only) -->
        <template v-else>
          <div class="group/name flex items-center gap-1">
            <span class="text-xs opacity-60 truncate max-w-[160px] pointer-events-none">{{ serverName }}</span>
            <button
              v-if="isOwner"
              class="btn btn-ghost btn-xs p-0 w-4 h-4 opacity-0 group-hover/name:opacity-50 hover:!opacity-100 transition-opacity"
              title="Rename server"
              @click="startEdit"
            >
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3 h-3" aria-hidden="true">
                <path d="M13.488 2.513a1.75 1.75 0 0 0-2.475 0L6.75 6.774a2.75 2.75 0 0 0-.596.892l-.848 2.047a.75.75 0 0 0 .98.98l2.047-.848a2.75 2.75 0 0 0 .892-.596l4.261-4.263a1.75 1.75 0 0 0 0-2.473ZM4.75 13.25a.75.75 0 0 0 0 1.5h7.5a.75.75 0 0 0 0-1.5h-7.5Z" />
              </svg>
            </button>
          </div>
        </template>
      </template>
    </div>

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
