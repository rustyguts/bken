<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import type { ChatMessage, Channel } from './types'

const props = defineProps<{
  messages: ChatMessage[]
  channels: Channel[]
  selectedChannelId: number
  myChannelId: number
  connected: boolean
  unreadCounts: Record<number, number>
  myId: number
  ownerId: number
}>()

const emit = defineEmits<{
  selectChannel: [channelID: number]
  send: [message: string]
  uploadFile: []
  uploadFileFromPath: [path: string]
  editMessage: [msgID: number, message: string]
  deleteMessage: [msgID: number]
}>()

const input = ref('')
const scrollEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
const uploading = ref(false)

// Inline editing state
const editingMsgId = ref<number | null>(null)
const editInput = ref('')
const editInputEl = ref<HTMLInputElement | null>(null)

const channelTabs = computed(() => [{ id: 0, name: 'Lobby' }, ...props.channels])

const selectedChannelName = computed(() => {
  const found = channelTabs.value.find(ch => ch.id === props.selectedChannelId)
  return found?.name ?? 'Lobby'
})

const isOwner = computed(() => props.ownerId !== 0 && props.ownerId === props.myId)

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

function canEdit(msg: ChatMessage): boolean {
  return msg.senderId === props.myId && !msg.deleted && !msg.fileUrl
}

function canDelete(msg: ChatMessage): boolean {
  if (msg.deleted) return false
  return msg.senderId === props.myId || isOwner.value
}

function startEdit(msg: ChatMessage): void {
  editingMsgId.value = msg.msgId
  editInput.value = msg.message
  nextTick(() => editInputEl.value?.focus())
}

function cancelEdit(): void {
  editingMsgId.value = null
  editInput.value = ''
}

function submitEdit(): void {
  const text = editInput.value.trim()
  if (!text || editingMsgId.value === null) {
    cancelEdit()
    return
  }
  emit('editMessage', editingMsgId.value, text)
  cancelEdit()
}

function confirmDelete(msg: ChatMessage): void {
  emit('deleteMessage', msg.msgId)
}

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

    <header class="border-b border-base-content/10 px-3 py-1.5 flex flex-col gap-1">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold"># {{ selectedChannelName }}</h2>
      </div>

      <div class="flex gap-1 overflow-x-auto">
        <button
          v-for="channel in channelTabs"
          :key="channel.id"
          class="btn btn-xs whitespace-nowrap relative"
          :class="channel.id === selectedChannelId ? 'btn-soft btn-primary' : 'btn-ghost'"
          @click="emit('selectChannel', channel.id)"
        >
          {{ channel.name }}
          <span
            v-if="unreadCounts[channel.id]"
            class="badge badge-xs badge-error absolute -top-1.5 -right-1.5 min-w-[16px] h-4 text-[10px] font-bold"
          >
            {{ unreadCounts[channel.id] > 99 ? '99+' : unreadCounts[channel.id] }}
          </span>
        </button>
      </div>
    </header>

    <div ref="scrollEl" class="flex-1 min-h-0 overflow-y-auto px-3 py-1 space-y-0.5">
      <div v-if="!connected" class="text-sm opacity-40 text-center pt-6">Connect to a server to start chatting</div>
      <div v-else-if="visibleMessages.length === 0" class="text-sm opacity-40 text-center pt-6">No messages in this channel yet</div>

      <article
        v-for="msg in visibleMessages"
        :key="msg.id"
        class="group py-1 px-1.5 rounded hover:bg-base-200 transition-colors relative"
      >
        <!-- Deleted message -->
        <div v-if="msg.deleted" class="flex items-baseline gap-2">
          <span class="text-xs font-semibold text-primary shrink-0">{{ msg.username }}</span>
          <span class="text-[11px] opacity-40 shrink-0">{{ formatTime(msg.ts) }}</span>
          <span class="text-sm italic opacity-40">message deleted</span>
        </div>

        <!-- Normal / edited message -->
        <template v-else>
          <!-- Inline edit mode -->
          <div v-if="editingMsgId === msg.msgId" class="flex items-center gap-2">
            <input
              ref="editInputEl"
              v-model="editInput"
              type="text"
              maxlength="500"
              class="input input-xs input-bordered flex-1"
              @keydown.enter.prevent="submitEdit"
              @keydown.escape.prevent="cancelEdit"
            />
            <button class="btn btn-xs btn-soft btn-primary" @click="submitEdit">Save</button>
            <button class="btn btn-xs btn-ghost" @click="cancelEdit">Cancel</button>
          </div>

          <!-- Normal display -->
          <div v-else class="flex items-baseline gap-2">
            <span class="text-xs font-semibold text-primary shrink-0">{{ msg.username }}</span>
            <span class="text-[11px] opacity-40 shrink-0">{{ formatTime(msg.ts) }}</span>
            <span v-if="msg.message" class="text-sm break-words">{{ msg.message }}</span>
            <span v-if="msg.edited" class="text-[10px] opacity-30 shrink-0">(edited)</span>

            <!-- Hover action icons -->
            <span
              v-if="canEdit(msg) || canDelete(msg)"
              class="ml-auto shrink-0 opacity-0 group-hover:opacity-100 transition-opacity flex gap-0.5"
            >
              <!-- Edit icon (pencil) -->
              <button
                v-if="canEdit(msg)"
                class="btn btn-ghost btn-xs btn-square"
                title="Edit message"
                @click="startEdit(msg)"
              >
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
                </svg>
              </button>
              <!-- Delete icon (trash) -->
              <button
                v-if="canDelete(msg)"
                class="btn btn-ghost btn-xs btn-square text-error/70 hover:text-error"
                title="Delete message"
                @click="confirmDelete(msg)"
              >
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                </svg>
              </button>
            </span>
          </div>

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

          <!-- Link preview -->
          <a
            v-if="msg.linkPreview"
            :href="msg.linkPreview.url"
            target="_blank"
            rel="noopener noreferrer"
            class="mt-2 block max-w-[400px] rounded-lg border border-base-content/10 bg-base-300 overflow-hidden hover:border-primary/30 transition-colors no-underline"
          >
            <img
              v-if="msg.linkPreview.image"
              :src="msg.linkPreview.image"
              :alt="msg.linkPreview.title"
              class="w-full max-h-[200px] object-cover"
              loading="lazy"
            />
            <div class="p-2">
              <p v-if="msg.linkPreview.siteName" class="text-[10px] opacity-50 uppercase tracking-wide mb-0.5">{{ msg.linkPreview.siteName }}</p>
              <p v-if="msg.linkPreview.title" class="text-sm font-semibold text-primary line-clamp-2">{{ msg.linkPreview.title }}</p>
              <p v-if="msg.linkPreview.description" class="text-xs opacity-70 mt-0.5 line-clamp-2">{{ msg.linkPreview.description }}</p>
            </div>
          </a>
        </template>
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
