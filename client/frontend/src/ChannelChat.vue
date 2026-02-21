<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import type { ChatMessage, Channel, User, ReactionInfo } from './types'
import { Pin, Search, Smile, Reply, Pencil, Trash2, FileText, X, Plus } from 'lucide-vue-next'

const props = defineProps<{
  messages: ChatMessage[]
  channels: Channel[]
  selectedChannelId: number
  myChannelId: number
  connected: boolean
  unreadCounts: Record<number, number>
  myId: number
  ownerId: number
  users?: User[]
  typingUsers?: Record<number, { username: string; channelId: number; expiresAt: number }>
  messageDensity?: 'compact' | 'default' | 'comfortable'
  showSystemMessages?: boolean
}>()

const emit = defineEmits<{
  selectChannel: [channelID: number]
  send: [message: string]
  uploadFile: []
  uploadFileFromPath: [path: string]
  editMessage: [msgID: number, message: string]
  deleteMessage: [msgID: number]
  addReaction: [msgID: number, emoji: string]
  removeReaction: [msgID: number, emoji: string]
}>()

const input = ref('')
const scrollEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
const uploading = ref(false)

// Inline editing state
const editingMsgId = ref<number | null>(null)
const editInput = ref('')
const editInputEl = ref<HTMLInputElement | null>(null)

// Image paste preview state
const pastedImage = ref<{ dataUrl: string; blob: Blob } | null>(null)

// @mention autocomplete state
const mentionQuery = ref('')
const mentionActive = ref(false)
const mentionIndex = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)

// Reply state
const replyingTo = ref<ChatMessage | null>(null)

// Search state
const searchOpen = ref(false)
const searchQuery = ref('')
const searchResults = ref<ChatMessage[]>([])

// Pinned panel state
const pinnedOpen = ref(false)

// Emoji reaction picker
const reactionPickerMsgId = ref<number | null>(null)
const commonEmojis = ['👍', '👎', '😂', '❤️', '🎉', '😮', '😢', '🔥', '👀', '🙏']

const density = computed(() => props.messageDensity ?? 'default')
const systemMsgsVisible = computed(() => props.showSystemMessages ?? true)

const selectedChannelName = computed(() => {
  const found = props.channels.find(ch => ch.id === props.selectedChannelId)
  return found?.name ?? (props.channels.length > 0 ? props.channels[0].name : 'General')
})

const isOwner = computed(() => props.ownerId !== 0 && props.ownerId === props.myId)

const visibleMessages = computed(() => {
  return props.messages.filter(msg => {
    if (msg.channelId !== props.selectedChannelId) return false
    if (msg.system && !systemMsgsVisible.value) return false
    return true
  })
})

const pinnedMessages = computed(() => {
  return visibleMessages.value.filter(m => m.pinned && !m.deleted)
})

// Typing indicators for current channel
const channelTypingUsers = computed(() => {
  if (!props.typingUsers) return []
  const now = Date.now()
  return Object.entries(props.typingUsers)
    .filter(([_, v]) => v.channelId === props.selectedChannelId && v.expiresAt > now)
    .map(([_, v]) => v.username)
})

const typingText = computed(() => {
  const names = channelTypingUsers.value
  if (names.length === 0) return ''
  if (names.length === 1) return `${names[0]} is typing...`
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing...`
  return `${names[0]} and ${names.length - 1} others are typing...`
})

// @mention autocomplete
const mentionSuggestions = computed(() => {
  if (!mentionActive.value || !mentionQuery.value || !props.users) return []
  const q = mentionQuery.value.toLowerCase()
  return props.users
    .filter(u => u.username.toLowerCase().includes(q) && u.id !== props.myId)
    .slice(0, 8)
})

// Check if a message mentions the current user
function isMentioned(msg: ChatMessage): boolean {
  return !!msg.mentions?.includes(props.myId)
}

watch(
  () => [visibleMessages.value.length, props.selectedChannelId],
  async () => {
    await nextTick()
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  },
)

function canEdit(msg: ChatMessage): boolean {
  return msg.senderId === props.myId && !msg.deleted && !msg.fileUrl && !msg.system
}

function canDelete(msg: ChatMessage): boolean {
  if (msg.deleted || msg.system) return false
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

function startReply(msg: ChatMessage): void {
  replyingTo.value = msg
  nextTick(() => inputEl.value?.focus())
}

function cancelReply(): void {
  replyingTo.value = null
}

function scrollToMessage(msgId: number): void {
  if (!scrollEl.value) return
  const el = scrollEl.value.querySelector(`[data-msg-id="${msgId}"]`)
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    el.classList.add('highlight-flash')
    setTimeout(() => el.classList.remove('highlight-flash'), 1500)
  }
}

function send(): void {
  const text = input.value.trim()
  if (!text || !props.connected) return
  emit('send', text)
  input.value = ''
  mentionActive.value = false
  replyingTo.value = null
}

function handleInput(e: Event): void {
  const target = e.target as HTMLInputElement
  const val = target.value
  const cursorPos = target.selectionStart ?? val.length

  // Check for @mention trigger
  const textBefore = val.slice(0, cursorPos)
  const atIdx = textBefore.lastIndexOf('@')
  if (atIdx >= 0 && (atIdx === 0 || textBefore[atIdx - 1] === ' ')) {
    const query = textBefore.slice(atIdx + 1)
    if (!query.includes(' ') && query.length > 0) {
      mentionQuery.value = query
      mentionActive.value = true
      mentionIndex.value = 0
      return
    }
  }
  mentionActive.value = false
}

function selectMention(username: string): void {
  const val = input.value
  const cursorPos = inputEl.value?.selectionStart ?? val.length
  const textBefore = val.slice(0, cursorPos)
  const atIdx = textBefore.lastIndexOf('@')
  if (atIdx >= 0) {
    input.value = val.slice(0, atIdx) + '@' + username + ' ' + val.slice(cursorPos)
  }
  mentionActive.value = false
  nextTick(() => inputEl.value?.focus())
}

function handleMentionKeydown(e: KeyboardEvent): void {
  if (!mentionActive.value || mentionSuggestions.value.length === 0) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    mentionIndex.value = (mentionIndex.value + 1) % mentionSuggestions.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    mentionIndex.value = (mentionIndex.value - 1 + mentionSuggestions.value.length) % mentionSuggestions.value.length
  } else if (e.key === 'Tab' || e.key === 'Enter') {
    if (mentionSuggestions.value.length > 0) {
      e.preventDefault()
      selectMention(mentionSuggestions.value[mentionIndex.value].username)
    }
  } else if (e.key === 'Escape') {
    mentionActive.value = false
  }
}

function handleKeydown(e: KeyboardEvent): void {
  if (mentionActive.value && mentionSuggestions.value.length > 0) {
    handleMentionKeydown(e)
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    send()
  }
}

function toggleReactionPicker(msgId: number): void {
  reactionPickerMsgId.value = reactionPickerMsgId.value === msgId ? null : msgId
}

function handleUploadClick(): void {
  if (!props.connected || uploading.value) return
  emit('uploadFile')
}

function handlePaste(e: ClipboardEvent): void {
  if (!props.connected) return
  const items = e.clipboardData?.items
  if (!items) return
  for (const item of items) {
    if (item.type.startsWith('image/')) {
      e.preventDefault()
      const blob = item.getAsFile()
      if (!blob) return
      const reader = new FileReader()
      reader.onload = () => {
        pastedImage.value = { dataUrl: reader.result as string, blob }
      }
      reader.readAsDataURL(blob)
      return
    }
  }
}

function cancelPastedImage(): void {
  pastedImage.value = null
}

function sendPastedImage(): void {
  if (!pastedImage.value) return
  pastedImage.value = null
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

function toggleSearch(): void {
  searchOpen.value = !searchOpen.value
  if (!searchOpen.value) {
    searchQuery.value = ''
    searchResults.value = []
  }
}

function doSearch(): void {
  if (!searchQuery.value.trim()) {
    searchResults.value = []
    return
  }
  const q = searchQuery.value.toLowerCase()
  searchResults.value = visibleMessages.value
    .filter(m => !m.deleted && !m.system && m.message.toLowerCase().includes(q))
    .reverse()
    .slice(0, 50)
}

function togglePinnedPanel(): void {
  pinnedOpen.value = !pinnedOpen.value
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

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
}

// Render message text with @mentions highlighted
function renderMessage(msg: ChatMessage): string {
  if (!msg.message) return ''
  let text = msg.message
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
  // Highlight @mentions
  if (msg.mentions && msg.mentions.length > 0 && props.users) {
    for (const uid of msg.mentions) {
      const user = props.users.find(u => u.id === uid)
      if (user) {
        const token = '@' + user.username.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
        const isSelf = uid === props.myId
        const cls = isSelf
          ? 'text-warning font-semibold bg-warning/15 px-0.5 rounded'
          : 'text-primary font-semibold bg-primary/10 px-0.5 rounded'
        text = text.replace(new RegExp(token, 'g'), `<span class="${cls}">@${user.username}</span>`)
      }
    }
  }
  return text
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
    <Transition
      enter-active-class="transition-opacity duration-150 ease-in-out"
      enter-from-class="opacity-0"
      leave-active-class="transition-opacity duration-150 ease-in-out"
      leave-to-class="opacity-0"
    >
      <div
        v-if="dragging"
        class="absolute inset-0 z-50 bg-primary/10 border-2 border-dashed border-primary rounded-lg flex items-center justify-center"
      >
        <div class="text-primary font-semibold text-lg">Drop file to upload</div>
      </div>
    </Transition>

    <header class="navbar min-h-0 h-auto border-b border-base-content/10 px-2 py-0.5 shrink-0">
      <div class="flex-1 flex flex-col gap-1">
        <div class="flex items-center gap-2">
          <h2 class="text-sm font-semibold"># {{ selectedChannelName }}</h2>
          <div class="ml-auto flex gap-1">
            <button
              v-if="pinnedMessages.length > 0"
              class="btn btn-ghost btn-xs"
              :class="pinnedOpen ? 'btn-active' : ''"
              title="Pinned messages"
              @click="togglePinnedPanel"
            >
              <Pin class="w-3.5 h-3.5" aria-hidden="true" />
              <span class="badge badge-xs">{{ pinnedMessages.length }}</span>
            </button>
            <button
              class="btn btn-ghost btn-xs"
              :class="searchOpen ? 'btn-active' : ''"
              title="Search messages"
              @click="toggleSearch"
            >
              <Search class="w-3.5 h-3.5" aria-hidden="true" />
            </button>
          </div>
        </div>

        <!-- Search bar -->
        <div v-if="searchOpen" class="join w-full">
          <input
            v-model="searchQuery"
            type="text"
            class="input input-xs join-item flex-1"
            placeholder="Search messages..."
            @input="doSearch"
            @keydown.escape="toggleSearch"
          />
          <button class="btn btn-xs btn-ghost join-item" @click="toggleSearch">Close</button>
        </div>
      </div>
    </header>

    <!-- Search results panel -->
    <ul v-if="searchOpen && searchResults.length > 0" class="menu menu-sm bg-base-200 max-h-[200px] overflow-y-auto border-b border-base-content/10">
      <li
        v-for="result in searchResults"
        :key="result.msgId"
      >
        <a @click="scrollToMessage(result.msgId); toggleSearch()">
          <span class="text-xs font-semibold text-primary">{{ result.username }}</span>
          <time class="text-[11px] opacity-40">{{ formatTime(result.ts) }}</time>
          <span class="text-xs opacity-70 truncate">{{ result.message }}</span>
        </a>
      </li>
    </ul>

    <!-- Pinned messages panel -->
    <div v-if="pinnedOpen" class="bg-base-200 max-h-[200px] overflow-y-auto border-b border-base-content/10">
      <div class="px-3 py-1 text-[11px] font-semibold opacity-50 uppercase tracking-wider">Pinned Messages</div>
      <ul class="menu menu-sm">
        <li
          v-for="msg in pinnedMessages"
          :key="msg.msgId"
        >
          <a @click="scrollToMessage(msg.msgId); pinnedOpen = false">
            <span class="text-xs font-semibold text-primary">{{ msg.username }}</span>
            <time class="text-[11px] opacity-40">{{ formatTime(msg.ts) }}</time>
            <span class="text-xs opacity-70 truncate">{{ msg.message }}</span>
          </a>
        </li>
      </ul>
    </div>

    <div ref="scrollEl" class="flex-1 min-h-0 overflow-y-auto px-3 py-0.5">
      <div v-if="!connected" class="text-sm opacity-40 text-center pt-6">Connect to a server to start chatting</div>
      <div v-else-if="visibleMessages.length === 0" class="text-sm opacity-40 text-center pt-6">No messages in this channel yet</div>

      <div :class="[density === 'compact' ? 'space-y-0' : density === 'comfortable' ? 'space-y-2' : 'space-y-0.5']">
      <div
        v-for="msg in visibleMessages"
        :key="msg.id"
        :data-msg-id="msg.msgId"
      >
        <!-- System message -->
        <div v-if="msg.system" class="text-center py-1">
          <span class="badge badge-ghost badge-sm italic">{{ msg.message }}</span>
        </div>

        <!-- Chat message: compact = IRC style, otherwise Discord style -->

        <!-- IRC style (compact) -->
        <div
          v-else-if="density === 'compact'"
          class="group flex items-baseline gap-1.5 px-1 py-px text-sm rounded hover:bg-base-content/5"
          :class="[
            isMentioned(msg) ? 'bg-warning/10 border-l-2 border-warning' : '',
            msg.pinned ? 'border-l-2 border-info/40' : '',
          ]"
        >
          <span class="font-semibold text-primary shrink-0">{{ msg.username }}:</span>
          <span v-if="msg.deleted" class="opacity-40 italic">message deleted</span>
          <span v-else-if="editingMsgId === msg.msgId" class="flex items-center gap-2 flex-1">
            <input
              ref="editInputEl"
              v-model="editInput"
              type="text"
              maxlength="500"
              class="input input-xs flex-1"
              @keydown.enter.prevent="submitEdit"
              @keydown.escape.prevent="cancelEdit"
            />
            <button class="btn btn-xs btn-primary" @click="submitEdit">Save</button>
            <button class="btn btn-xs btn-ghost" @click="cancelEdit">Cancel</button>
          </span>
          <span v-else v-html="renderMessage(msg)" />
          <span v-if="msg.edited" class="text-[10px] opacity-30 shrink-0">(edited)</span>
          <!-- Hover actions -->
          <span class="opacity-0 group-hover:opacity-100 transition-opacity flex gap-0.5 ml-auto shrink-0">
            <button class="btn btn-ghost btn-xs btn-square" title="React" @click.stop="toggleReactionPicker(msg.msgId)">
              <Smile class="w-3 h-3" aria-hidden="true" />
            </button>
            <button class="btn btn-ghost btn-xs btn-square" title="Reply" @click="startReply(msg)">
              <Reply class="w-3 h-3" aria-hidden="true" />
            </button>
            <button v-if="canEdit(msg)" class="btn btn-ghost btn-xs btn-square" title="Edit" @click="startEdit(msg)">
              <Pencil class="w-3 h-3" aria-hidden="true" />
            </button>
            <button v-if="canDelete(msg)" class="btn btn-ghost btn-xs btn-square text-error/70 hover:text-error" title="Delete" @click="confirmDelete(msg)">
              <Trash2 class="w-3 h-3" aria-hidden="true" />
            </button>
          </span>
          <!-- Reaction picker -->
          <div v-if="reactionPickerMsgId === msg.msgId" class="flex gap-0.5 flex-wrap p-1 bg-base-300 rounded-lg w-fit">
            <button
              v-for="emoji in commonEmojis"
              :key="emoji"
              class="btn btn-ghost btn-xs btn-square text-base"
              @click="$emit('addReaction', msg.msgId, emoji); reactionPickerMsgId = null"
            >{{ emoji }}</button>
          </div>
        </div>

        <!-- Discord style (default / comfortable) -->
        <div
          v-else
          class="group flex gap-3 px-2 py-1 rounded hover:bg-base-content/5"
          :class="[
            isMentioned(msg) ? 'bg-warning/10 border-l-2 border-warning' : '',
            msg.pinned ? 'border-l-2 border-info/40' : '',
          ]"
        >
          <!-- Avatar -->
          <div class="shrink-0 pt-0.5">
            <div class="avatar avatar-placeholder">
              <div
                class="bg-neutral text-neutral-content rounded-full"
                :class="density === 'comfortable' ? 'w-10' : 'w-9'"
              >
                <span class="text-xs">{{ initials(msg.username) }}</span>
              </div>
            </div>
          </div>

          <!-- Content -->
          <div class="flex-1 min-w-0">
            <!-- Header row -->
            <div class="flex items-baseline gap-2 leading-tight">
              <span class="font-semibold text-primary text-sm">{{ msg.username }}</span>
              <time class="text-xs opacity-40">{{ formatTime(msg.ts) }}</time>
              <span v-if="msg.edited" class="text-[10px] opacity-30">(edited)</span>
              <span v-if="msg.pinned" class="badge badge-info badge-xs">pinned</span>
              <!-- Hover action icons -->
              <span
                v-if="!msg.deleted"
                class="opacity-0 group-hover:opacity-100 transition-opacity flex gap-0.5 ml-auto"
              >
                <button class="btn btn-ghost btn-xs btn-square" title="React" @click.stop="toggleReactionPicker(msg.msgId)">
                  <Smile class="w-3.5 h-3.5" aria-hidden="true" />
                </button>
                <button class="btn btn-ghost btn-xs btn-square" title="Reply" @click="startReply(msg)">
                  <Reply class="w-3.5 h-3.5" aria-hidden="true" />
                </button>
                <button v-if="canEdit(msg)" class="btn btn-ghost btn-xs btn-square" title="Edit message" @click="startEdit(msg)">
                  <Pencil class="w-3.5 h-3.5" aria-hidden="true" />
                </button>
                <button v-if="canDelete(msg)" class="btn btn-ghost btn-xs btn-square text-error/70 hover:text-error" title="Delete message" @click="confirmDelete(msg)">
                  <Trash2 class="w-3.5 h-3.5" aria-hidden="true" />
                </button>
              </span>
            </div>

            <!-- Reply preview -->
            <div v-if="msg.replyPreview" class="text-xs opacity-60 cursor-pointer mb-0.5 flex items-center gap-1 border-l-2 border-base-content/20 pl-2" @click="scrollToMessage(msg.replyPreview.msg_id)">
              <span class="font-semibold text-primary">{{ msg.replyPreview.username }}:</span>
              <span v-if="msg.replyPreview.deleted" class="italic">message deleted</span>
              <span v-else class="truncate max-w-[250px]">{{ msg.replyPreview.message }}</span>
            </div>

            <!-- Message content -->
            <div v-if="msg.deleted" class="text-sm opacity-40 italic">message deleted</div>
            <template v-else>
              <!-- Inline edit -->
              <div v-if="editingMsgId === msg.msgId" class="flex items-center gap-2 mt-0.5">
                <input
                  ref="editInputEl"
                  v-model="editInput"
                  type="text"
                  maxlength="500"
                  class="input input-xs flex-1"
                  @keydown.enter.prevent="submitEdit"
                  @keydown.escape.prevent="cancelEdit"
                />
                <button class="btn btn-xs btn-primary" @click="submitEdit">Save</button>
                <button class="btn btn-xs btn-ghost" @click="cancelEdit">Cancel</button>
              </div>
              <!-- Text -->
              <div v-else class="text-sm leading-snug">
                <span v-if="msg.message" v-html="renderMessage(msg)" />
              </div>
            </template>

            <!-- Reaction picker -->
            <div v-if="reactionPickerMsgId === msg.msgId" class="flex gap-0.5 flex-wrap mt-1 p-1 bg-base-300 rounded-lg w-fit">
              <button
                v-for="emoji in commonEmojis"
                :key="emoji"
                class="btn btn-ghost btn-xs btn-square text-base"
                @click="$emit('addReaction', msg.msgId, emoji); reactionPickerMsgId = null"
              >{{ emoji }}</button>
            </div>

            <!-- Reactions display -->
            <div v-if="msg.reactions && msg.reactions.length > 0" class="flex flex-wrap gap-1 mt-1">
              <button
                v-for="rx in msg.reactions"
                :key="rx.emoji"
                class="badge badge-sm cursor-pointer"
                :class="rx.user_ids.includes(myId) ? 'badge-primary badge-outline' : 'badge-ghost'"
                :title="rx.user_ids.map(id => users?.find(u => u.id === id)?.username ?? 'Unknown').join(', ')"
                @click="rx.user_ids.includes(myId) ? $emit('removeReaction', msg.msgId, rx.emoji) : $emit('addReaction', msg.msgId, rx.emoji)"
              >
                {{ rx.emoji }} {{ rx.count }}
              </button>
            </div>

            <!-- File attachment -->
            <div v-if="msg.fileUrl" class="mt-1">
              <a
                v-if="msg.fileName && isImageFile(msg.fileName)"
                :href="msg.fileUrl"
                target="_blank"
                class="block"
              >
                <img
                  :src="msg.fileUrl"
                  :alt="msg.fileName"
                  class="max-w-[240px] max-h-[180px] rounded border border-base-content/10 object-contain"
                  loading="lazy"
                />
                <span class="text-[11px] opacity-50 mt-0.5 block">
                  {{ msg.fileName }} ({{ formatFileSize(msg.fileSize ?? 0) }})
                </span>
              </a>
              <a
                v-else
                :href="msg.fileUrl"
                target="_blank"
                class="btn btn-ghost btn-sm gap-2 normal-case"
              >
                <FileText class="w-4 h-4 opacity-60" aria-hidden="true" />
                <span class="truncate max-w-[200px]">{{ msg.fileName }}</span>
                <span class="badge badge-ghost badge-xs">{{ formatFileSize(msg.fileSize ?? 0) }}</span>
              </a>
            </div>

            <!-- Link preview -->
            <a
              v-if="msg.linkPreview"
              :href="msg.linkPreview.url"
              target="_blank"
              rel="noopener noreferrer"
              class="card card-compact bg-base-300 mt-2 max-w-[400px] hover:border-primary/30 transition-colors no-underline"
            >
              <figure v-if="msg.linkPreview.image">
                <img
                  :src="msg.linkPreview.image"
                  :alt="msg.linkPreview.title"
                  class="w-full max-h-[200px] object-cover"
                  loading="lazy"
                />
              </figure>
              <div class="card-body">
                <p v-if="msg.linkPreview.siteName" class="text-[10px] opacity-50 uppercase tracking-wide">{{ msg.linkPreview.siteName }}</p>
                <h3 v-if="msg.linkPreview.title" class="card-title text-sm text-primary">{{ msg.linkPreview.title }}</h3>
                <p v-if="msg.linkPreview.description" class="text-xs opacity-70 line-clamp-2">{{ msg.linkPreview.description }}</p>
              </div>
            </a>
          </div>
        </div>
      </div>
      </div>
    </div>

    <!-- Typing indicator -->
    <div v-if="typingText" class="px-3 py-0.5 text-[11px] opacity-50 italic border-t border-base-content/5 flex items-center gap-1">
      <span class="loading loading-dots loading-xs"></span>
      {{ typingText }}
    </div>

    <!-- Pasted image preview -->
    <div v-if="pastedImage" class="alert border-t border-base-content/10 rounded-none flex items-center gap-3">
      <img :src="pastedImage.dataUrl" alt="Pasted image" class="max-w-[120px] max-h-[80px] rounded border border-base-content/10 object-contain" />
      <div class="flex gap-1.5">
        <button class="btn btn-xs btn-primary" @click="sendPastedImage">Send</button>
        <button class="btn btn-xs btn-ghost" @click="cancelPastedImage">Cancel</button>
      </div>
    </div>

    <!-- Reply preview bar -->
    <div v-if="replyingTo" class="alert alert-info rounded-none py-1 flex items-center gap-2 text-xs">
      <span class="opacity-50">Replying to</span>
      <span class="font-semibold">{{ replyingTo.username }}</span>
      <span class="opacity-50 truncate max-w-[200px]">{{ replyingTo.message }}</span>
      <button class="btn btn-ghost btn-xs btn-square ml-auto" @click="cancelReply">
        <X class="w-3 h-3" aria-hidden="true" />
      </button>
    </div>

    <footer class="border-t border-base-content/10 px-2 py-1 relative">
      <!-- @mention autocomplete popup -->
      <ul v-if="mentionActive && mentionSuggestions.length > 0" class="menu menu-sm bg-base-200 rounded-box shadow-lg border border-base-content/10 absolute bottom-full left-0 right-0 mx-2 mb-1 max-h-[200px] overflow-y-auto z-40">
        <li
          v-for="(user, idx) in mentionSuggestions"
          :key="user.id"
        >
          <a
            :class="idx === mentionIndex ? 'active' : ''"
            @click="selectMention(user.username)"
          >
            @{{ user.username }}
          </a>
        </li>
      </ul>

      <div class="join w-full">
        <button
          class="btn btn-sm btn-ghost join-item"
          :disabled="!connected || uploading"
          title="Upload file"
          @click="handleUploadClick"
        >
          <Plus class="w-4 h-4" aria-hidden="true" />
        </button>
        <input
          ref="inputEl"
          v-model="input"
          type="text"
          maxlength="500"
          class="input input-sm join-item flex-1"
          :placeholder="connected ? `Message #${selectedChannelName}` : 'Disconnected'"
          :disabled="!connected"
          @keydown="handleKeydown"
          @input="handleInput"
          @paste="handlePaste"
        />
      </div>
    </footer>
  </section>
</template>

