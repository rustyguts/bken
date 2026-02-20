<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import type { ChatMessage, Channel } from './types'

const props = defineProps<{
  messages: ChatMessage[]
  channels: Channel[]
  selectedChannelId: number
  myChannelId: number
  connected: boolean
}>()

const emit = defineEmits<{
  selectChannel: [channelID: number]
  send: [message: string]
  uploadFile: []
  uploadFileFromPath: [path: string]
}>()

const input = ref('')
const scrollEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
const uploading = ref(false)

const channelTabs = computed(() => [{ id: 0, name: 'Lobby' }, ...props.channels])

const selectedChannelName = computed(() => {
  const found = channelTabs.value.find(ch => ch.id === props.selectedChannelId)
  return found?.name ?? 'Lobby'
})

const visibleMessages = computed(() => {
  return props.messages.filter(msg => msg.channelId === props.selectedChannelId)
})

watch(
  () => [visibleMessages.value.length, props.selectedChannelId],
  async () => {
    await nextTick()
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  },
)

function send(): void {
  const text = input.value.trim()
  if (!text || !props.connected) return
  emit('send', text)
  input.value = ''
}

function handleUploadClick(): void {
  if (!props.connected || uploading.value) return
  emit('uploadFile')
}

function onDragOver(e: DragEvent): void {
  if (!props.connected) return
  e.preventDefault()
  dragging.value = true
}

function onDragLeave(): void {
  dragging.value = false
}

function onDrop(e: DragEvent): void {
  e.preventDefault()
  dragging.value = false
  // Wails handles the native file drop via OnFileDrop; the CSS drop-target
  // attribute tells Wails which element is the target. The file:dropped event
  // is emitted from Go and handled in App.vue.
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString('en', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function isImageFile(name: string): boolean {
  const ext = name.toLowerCase().split('.').pop() ?? ''
  return ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp'].includes(ext)
}
</script>

<template>
  <section
    class="flex flex-col h-full min-h-0 bg-base-100 relative"
    style="--wails-drop-target: drop"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <!-- Drag overlay -->
    <Transition name="fade">
      <div
        v-if="dragging"
        class="absolute inset-0 z-50 bg-primary/10 border-2 border-dashed border-primary rounded-lg flex items-center justify-center"
      >
        <div class="text-primary font-semibold text-lg">Drop file to upload</div>
      </div>
    </Transition>

    <header class="border-b border-base-content/10 px-3 py-2 flex flex-col gap-2">
      <div class="flex items-center justify-between">
        <h2 class="text-sm font-semibold"># {{ selectedChannelName }}</h2>
        <span class="text-[11px] opacity-50">Chatroom</span>
      </div>

      <div class="flex gap-1 overflow-x-auto pb-1">
        <button
          v-for="channel in channelTabs"
          :key="channel.id"
          class="btn btn-xs whitespace-nowrap"
          :class="channel.id === selectedChannelId ? 'btn-primary' : 'btn-ghost'"
          @click="emit('selectChannel', channel.id)"
        >
          {{ channel.name }}
        </button>
      </div>
    </header>

    <div ref="scrollEl" class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <div v-if="!connected" class="text-sm opacity-40 text-center pt-6">Connect to a server to start chatting</div>
      <div v-else-if="visibleMessages.length === 0" class="text-sm opacity-40 text-center pt-6">No messages in this channel yet</div>

      <article
        v-for="msg in visibleMessages"
        :key="msg.id"
        class="rounded-lg border border-base-content/10 bg-base-200 p-2"
      >
        <div class="flex items-center gap-2 mb-1">
          <span class="text-xs font-semibold text-primary">{{ msg.username }}</span>
          <span class="text-[11px] opacity-50">{{ formatTime(msg.ts) }}</span>
        </div>
        <p v-if="msg.message" class="text-sm break-words">{{ msg.message }}</p>

        <!-- File attachment -->
        <div v-if="msg.fileUrl" class="mt-1">
          <!-- Image preview -->
          <a
            v-if="msg.fileName && isImageFile(msg.fileName)"
            :href="msg.fileUrl"
            target="_blank"
            class="block"
          >
            <img
              :src="msg.fileUrl"
              :alt="msg.fileName"
              class="max-w-[320px] max-h-[240px] rounded border border-base-content/10 object-contain"
              loading="lazy"
            />
            <span class="text-[11px] opacity-50 mt-0.5 block">
              {{ msg.fileName }} ({{ formatFileSize(msg.fileSize ?? 0) }})
            </span>
          </a>
          <!-- Generic file download link -->
          <a
            v-else
            :href="msg.fileUrl"
            target="_blank"
            class="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-base-300 hover:bg-base-content/10 transition-colors text-sm"
          >
            <svg class="w-4 h-4 opacity-60 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /><polyline points="10 9 9 9 8 9" />
            </svg>
            <span class="truncate max-w-[200px]">{{ msg.fileName }}</span>
            <span class="text-[11px] opacity-50 shrink-0">({{ formatFileSize(msg.fileSize ?? 0) }})</span>
          </a>
        </div>
      </article>
    </div>

    <footer class="border-t border-base-content/10 p-2 flex gap-2">
      <button
        class="btn btn-sm btn-ghost btn-square shrink-0"
        :disabled="!connected || uploading"
        title="Upload file"
        @click="handleUploadClick"
      >
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
        </svg>
      </button>
      <input
        v-model="input"
        type="text"
        maxlength="500"
        class="input input-sm input-bordered w-full"
        :placeholder="connected ? `Message #${selectedChannelName}` : 'Disconnected'"
        :disabled="!connected"
        @keydown.enter.prevent="send"
      />
    </footer>
  </section>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
