<script setup lang="ts">
import { computed, ref, nextTick } from 'vue'
import type { Channel, User } from './types'

const props = defineProps<{
  channels: Channel[]
  users: User[]
  userChannels: Record<number, number>
  myId: number
  selectedChannelId: number
  serverName: string
  speakingUsers: Set<number>
  connectError: string
  isOwner: boolean
  unreadCounts: Record<number, number>
}>()

const emit = defineEmits<{
  join: [channelID: number]
  select: [channelID: number]
  createChannel: [name: string]
  renameChannel: [channelID: number, name: string]
  deleteChannel: [channelID: number]
  moveUser: [userID: number, channelID: number]
}>()

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)
const rows = computed(() => [{ id: 0, name: 'Lobby' }, ...props.channels])
const hasMyChannelState = computed(() => Object.prototype.hasOwnProperty.call(props.userChannels, props.myId))
const hasMeInUserList = computed(() => props.users.some(u => u.id === props.myId))

// Create channel state
const showCreateDialog = ref(false)
const newChannelName = ref('')
const createInputRef = ref<HTMLInputElement | null>(null)

// Context menu state
const contextMenu = ref<{ x: number; y: number; channel: Channel } | null>(null)

// Rename state
const renamingChannelId = ref<number | null>(null)
const renameValue = ref('')
const renameInputRef = ref<HTMLInputElement | null>(null)

function usersForChannel(channelId: number): User[] {
  const users = props.users.filter(u => (props.userChannels[u.id] ?? 0) === channelId)
  if (props.myId > 0 && hasMyChannelState.value && !hasMeInUserList.value && myChannelId.value === channelId) {
    return [...users, { id: props.myId, username: 'You' }]
  }
  return users
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '??'
  const a = parts[0][0] ?? ''
  const b = parts[1]?.[0] ?? parts[0][1] ?? ''
  return `${a}${b}`.toUpperCase()
}

function selectChannel(channelId: number): void {
  emit('select', channelId)
}

function joinVoice(channelId: number, event: MouseEvent): void {
  event.stopPropagation()
  emit('join', channelId)
}

function isConnectedToChannel(channelId: number): boolean {
  return props.myId > 0 && hasMyChannelState.value && myChannelId.value === channelId
}

// Create channel
function openCreateDialog(): void {
  newChannelName.value = ''
  showCreateDialog.value = true
  nextTick(() => createInputRef.value?.focus())
}

function confirmCreate(): void {
  const name = newChannelName.value.trim()
  if (!name) return
  emit('createChannel', name)
  showCreateDialog.value = false
  newChannelName.value = ''
}

function cancelCreate(): void {
  showCreateDialog.value = false
  newChannelName.value = ''
}

// Context menu
function openContextMenu(event: MouseEvent, channel: { id: number; name: string }): void {
  if (!props.isOwner || channel.id === 0) return
  event.preventDefault()
  contextMenu.value = { x: event.clientX, y: event.clientY, channel: channel as Channel }
}

function closeContextMenu(): void {
  contextMenu.value = null
}

// Rename channel
function startRename(): void {
  if (!contextMenu.value) return
  renamingChannelId.value = contextMenu.value.channel.id
  renameValue.value = contextMenu.value.channel.name
  closeContextMenu()
  nextTick(() => renameInputRef.value?.select())
}

function confirmRename(): void {
  if (renamingChannelId.value === null) return
  const name = renameValue.value.trim()
  if (name && name !== props.channels.find(c => c.id === renamingChannelId.value)?.name) {
    emit('renameChannel', renamingChannelId.value, name)
  }
  renamingChannelId.value = null
  renameValue.value = ''
}

function cancelRename(): void {
  renamingChannelId.value = null
  renameValue.value = ''
}

function handleRenameKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter') { e.preventDefault(); confirmRename() }
  else if (e.key === 'Escape') cancelRename()
}

// Delete channel
function startDelete(): void {
  if (!contextMenu.value) return
  const channel = contextMenu.value.channel
  closeContextMenu()
  emit('deleteChannel', channel.id)
}

// User context menu (owner right-click on user avatar to move them)
const userContextMenu = ref<{ x: number; y: number; user: User; currentChannelId: number } | null>(null)

function openUserContextMenu(event: MouseEvent, user: User, currentChannelId: number): void {
  if (!props.isOwner || user.id === props.myId) return
  event.preventDefault()
  event.stopPropagation()
  userContextMenu.value = { x: event.clientX, y: event.clientY, user, currentChannelId }
}

function closeUserContextMenu(): void {
  userContextMenu.value = null
}

function moveUserToChannel(channelId: number): void {
  if (!userContextMenu.value) return
  emit('moveUser', userContextMenu.value.user.id, channelId)
  closeUserContextMenu()
}

/** Channels the user can be moved to (all rows except their current one). */
const moveTargets = computed(() => {
  if (!userContextMenu.value) return []
  const current = userContextMenu.value.currentChannelId
  return rows.value.filter(ch => ch.id !== current)
})
</script>

<template>
  <section class="flex flex-col h-full min-h-0" @click="closeContextMenu(); closeUserContextMenu()">
    <div class="px-3 py-2 border-b border-base-content/10 flex items-center justify-between">
      <h2 class="text-xs font-semibold uppercase tracking-widest opacity-50">{{ serverName || 'Server' }}</h2>
      <button
        v-if="isOwner"
        class="btn btn-ghost btn-xs p-0 w-5 h-5 opacity-40 hover:opacity-100 transition-opacity"
        title="Create channel"
        @click="openCreateDialog"
      >
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3.5 h-3.5" aria-hidden="true">
          <path d="M8.75 3.75a.75.75 0 0 0-1.5 0v3.5h-3.5a.75.75 0 0 0 0 1.5h3.5v3.5a.75.75 0 0 0 1.5 0v-3.5h3.5a.75.75 0 0 0 0-1.5h-3.5v-3.5Z" />
        </svg>
      </button>
    </div>

    <div v-if="connectError" class="mx-2 mt-2 rounded-md bg-error/10 border border-error/30 px-3 py-2 text-xs text-error">
      {{ connectError }}
    </div>

    <div class="flex-1 min-h-0 overflow-y-auto p-2 space-y-2">
      <div
        v-for="channel in rows"
        :key="channel.id"
        class="group w-full text-left rounded-md px-2 py-2 border transition cursor-pointer"
        :class="[
          selectedChannelId === channel.id ? 'bg-primary/15 border-primary/40' : 'bg-base-200 border-base-content/10 hover:border-base-content/30',
          isConnectedToChannel(channel.id) ? 'ring-1 ring-primary/30' : '',
        ]"
        @click="selectChannel(channel.id)"
        @contextmenu="openContextMenu($event, channel)"
      >
        <div class="flex items-center gap-2">
          <span class="text-xs opacity-70">{{ isConnectedToChannel(channel.id) ? '●' : '○' }}</span>

          <!-- Rename inline input -->
          <template v-if="renamingChannelId === channel.id">
            <input
              ref="renameInputRef"
              v-model="renameValue"
              class="input input-ghost input-xs text-sm h-5 flex-1 min-w-0 px-1 py-0 focus:outline-none bg-base-100/40 rounded"
              maxlength="50"
              @keydown="handleRenameKeydown"
              @blur="cancelRename"
              @click.stop
            />
            <button
              class="btn btn-ghost btn-xs p-0 w-4 h-4 text-success opacity-70 hover:opacity-100"
              title="Save"
              tabindex="-1"
              @mousedown.prevent="confirmRename"
            >
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3 h-3" aria-hidden="true">
                <path fill-rule="evenodd" d="M12.416 3.376a.75.75 0 0 1 .208 1.04l-5 7.5a.75.75 0 0 1-1.154.114l-3-3a.75.75 0 0 1 1.06-1.06l2.353 2.353 4.493-6.74a.75.75 0 0 1 1.04-.207Z" clip-rule="evenodd" />
              </svg>
            </button>
          </template>

          <template v-else>
            <span class="text-sm font-medium truncate">{{ channel.name }}</span>
          </template>

          <span
            v-if="unreadCounts[channel.id]"
            class="badge badge-xs badge-error font-bold min-w-[16px]"
          >
            {{ unreadCounts[channel.id] > 99 ? '99+' : unreadCounts[channel.id] }}
          </span>
          <span class="badge badge-ghost badge-xs ml-auto">{{ usersForChannel(channel.id).length }}</span>
        </div>

        <div class="mt-2 flex items-center flex-wrap gap-1">
          <span
            v-for="user in usersForChannel(channel.id).slice(0, 6)"
            :key="`${channel.id}-${user.id}`"
            class="w-5 h-5 rounded-full border text-[9px] font-mono flex items-center justify-center transition-all duration-150"
            :class="[
              speakingUsers.has(user.id) ? 'bg-success/20 border-success ring-1 ring-success/50' : 'bg-base-300 border-base-content/20',
              isOwner && user.id !== myId ? 'cursor-context-menu' : '',
            ]"
            :title="user.username"
            @contextmenu="openUserContextMenu($event, user, channel.id)"
          >
            {{ initials(user.username) }}
          </span>
          <span
            v-if="usersForChannel(channel.id).length > 6"
            class="badge badge-ghost badge-xs"
            :title="`${usersForChannel(channel.id).length - 6} more users`"
          >
            +{{ usersForChannel(channel.id).length - 6 }}
          </span>
          <span v-if="usersForChannel(channel.id).length === 0" class="text-[10px] opacity-40 italic">No users</span>

          <!-- Join Voice button (hover reveal, hidden if already in this channel) -->
          <button
            v-if="!isConnectedToChannel(channel.id)"
            class="btn btn-xs btn-primary ml-auto opacity-0 group-hover:opacity-100 transition-opacity"
            title="Connect to voice"
            @click="joinVoice(channel.id, $event)"
          >
            Join Voice
          </button>
          <span v-else class="text-[10px] ml-auto text-success opacity-80">Voice Connected</span>
        </div>
      </div>
    </div>

    <!-- Context menu (owner right-click on channel) -->
    <Teleport to="body">
      <div
        v-if="contextMenu"
        class="fixed z-50 min-w-[140px] rounded-lg border border-base-content/15 bg-base-200 shadow-lg py-1"
        :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }"
        @click.stop
      >
        <button
          class="w-full text-left px-3 py-1.5 text-xs hover:bg-base-content/10 transition-colors"
          @click="startRename"
        >
          Rename Channel
        </button>
        <button
          class="w-full text-left px-3 py-1.5 text-xs text-error hover:bg-error/10 transition-colors"
          @click="startDelete"
        >
          Delete Channel
        </button>
      </div>
    </Teleport>

    <!-- User context menu (owner right-click on user to move them) -->
    <Teleport to="body">
      <div
        v-if="userContextMenu"
        class="fixed z-50 min-w-[160px] rounded-lg border border-base-content/15 bg-base-200 shadow-lg py-1"
        :style="{ left: userContextMenu.x + 'px', top: userContextMenu.y + 'px' }"
        @click.stop
      >
        <div class="px-3 py-1 text-[10px] uppercase tracking-wider opacity-40 select-none">
          Move {{ userContextMenu.user.username }}
        </div>
        <button
          v-for="target in moveTargets"
          :key="target.id"
          class="w-full text-left px-3 py-1.5 text-xs hover:bg-base-content/10 transition-colors"
          @click="moveUserToChannel(target.id)"
        >
          {{ target.name }}
        </button>
      </div>
    </Teleport>

    <!-- Create channel dialog -->
    <dialog class="modal" :class="{ 'modal-open': showCreateDialog }">
      <div class="modal-box w-80">
        <h3 class="text-sm font-semibold mb-3">Create Channel</h3>
        <input
          ref="createInputRef"
          v-model="newChannelName"
          type="text"
          placeholder="Channel name"
          class="input input-sm input-bordered w-full"
          maxlength="50"
          @keydown.enter.prevent="confirmCreate"
          @keydown.escape.prevent="cancelCreate"
        />
        <div class="mt-3 flex gap-2 justify-end">
          <button class="btn btn-ghost btn-sm" @click="cancelCreate">Cancel</button>
          <button class="btn btn-primary btn-sm" :disabled="!newChannelName.trim()" @click="confirmCreate">Create</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="cancelCreate">
        <button>close</button>
      </form>
    </dialog>
  </section>
</template>
