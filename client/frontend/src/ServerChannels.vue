<script setup lang="ts">
import { computed, ref, nextTick } from 'vue'
import type { Channel, User } from './types'
import UserProfilePopup from './UserProfilePopup.vue'
import { SetUserVolume, GetUserVolume, StartRecording, StopRecording, RenameServer } from './config'
import { BKEN_SCHEME } from './constants'
import { Volume2, Plus, Settings, Check, Square, Circle, ChevronDown, Video, Monitor } from 'lucide-vue-next'

const props = defineProps<{
  channels: Channel[]
  users: User[]
  userChannels: Record<number, number>
  myId: number
  connectedAddr?: string
  selectedChannelId: number
  serverName: string
  speakingUsers: Set<number>
  voiceConnected: boolean
  videoActive: boolean
  screenSharing: boolean
  connectError: string
  isOwner: boolean
  unreadCounts: Record<number, number>
  ownerId: number
  recordingChannels: Record<number, { recording: boolean; startedBy: string }>
}>()

const emit = defineEmits<{
  join: [channelID: number]
  select: [channelID: number]
  createChannel: [name: string]
  renameChannel: [channelID: number, name: string]
  deleteChannel: [channelID: number]
  moveUser: [userID: number, channelID: number]
  kickUser: [userID: number]
  'video-toggle': []
  'screen-share-toggle': []
}>()

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)
const rows = computed(() => props.channels)
const hasMyChannelState = computed(() => Object.prototype.hasOwnProperty.call(props.userChannels, props.myId))
const hasMeInUserList = computed(() => props.users.some(u => u.id === props.myId))
const myUser = computed(() => props.users.find(u => u.id === props.myId))
const myRole = computed(() => (myUser.value?.role ?? '').toUpperCase())

function hostFromAddr(raw?: string): string {
  if (!raw) return ''
  let s = raw.trim().toLowerCase()
  if (!s) return ''
  if (s.startsWith(BKEN_SCHEME)) s = s.slice(BKEN_SCHEME.length)
  const slash = s.indexOf('/')
  if (slash >= 0) s = s.slice(0, slash)
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end > 1) return s.slice(1, end)
  }
  const firstColon = s.indexOf(':')
  const lastColon = s.lastIndexOf(':')
  if (firstColon >= 0 && firstColon === lastColon) {
    return s.slice(0, firstColon)
  }
  return s
}

const isDevLocalServer = computed(() => {
  if (!import.meta.env.DEV) return false
  const h = hostFromAddr(props.connectedAddr)
  return h === 'localhost' || h === '127.0.0.1' || h === '::1' || h === '0.0.0.0'
})

const canOpenServerAdminSettings = computed(() =>
  props.isOwner || myRole.value === 'OWNER' || myRole.value === 'ADMIN' || isDevLocalServer.value,
)
const canCreateChannels = computed(() => canOpenServerAdminSettings.value)
const canRenameServer = computed(() => props.isOwner || myRole.value === 'OWNER')

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

// Server admin settings modal
const showServerAdminModal = ref(false)
const serverNameDraft = ref('')
const serverOptionsError = ref('')
const savingServerName = ref(false)

function usersForChannel(channelId: number): User[] {
  const users = props.users.filter(u => (props.userChannels[u.id] ?? 0) === channelId)
  if (props.myId > 0 && hasMyChannelState.value && !hasMeInUserList.value && myChannelId.value === channelId) {
    return [...users, { id: props.myId, username: 'You' }]
  }
  return users
}

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
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
  if (!canCreateChannels.value) return
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

function openServerAdminModal(): void {
  closeContextMenu()
  closeUserContextMenu()
  serverNameDraft.value = props.serverName || ''
  serverOptionsError.value = ''
  showServerAdminModal.value = true
}

function closeServerAdminModal(): void {
  showServerAdminModal.value = false
  serverOptionsError.value = ''
}

async function saveServerName(): Promise<void> {
  if (!canRenameServer.value || savingServerName.value) return
  const name = serverNameDraft.value.trim()
  if (!name) {
    serverOptionsError.value = 'Server name cannot be empty.'
    return
  }
  savingServerName.value = true
  serverOptionsError.value = ''
  try {
    const err = await RenameServer(name)
    if (err) {
      serverOptionsError.value = err
      return
    }
    showServerAdminModal.value = false
  } finally {
    savingServerName.value = false
  }
}

// Context menu
function openContextMenu(event: MouseEvent, channel: { id: number; name: string }): void {
  if (!props.isOwner) return
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

// User context menu (right-click on user avatar: volume for all, move/kick for owner)
const userContextMenu = ref<{ x: number; y: number; user: User; currentChannelId: number } | null>(null)
const userVolume = ref(100) // 0-200%

async function openUserContextMenu(event: MouseEvent, user: User, currentChannelId: number): Promise<void> {
  if (user.id === props.myId) return
  event.preventDefault()
  event.stopPropagation()
  // Fetch the user's current volume.
  try {
    const vol = await GetUserVolume(user.id)
    userVolume.value = Math.round(vol * 100)
  } catch {
    userVolume.value = 100
  }
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

function kickUser(): void {
  if (!userContextMenu.value) return
  emit('kickUser', userContextMenu.value.user.id)
  closeUserContextMenu()
}

async function handleUserVolumeChange(): Promise<void> {
  if (!userContextMenu.value) return
  await SetUserVolume(userContextMenu.value.user.id, userVolume.value / 100)
}

/** Channels the user can be moved to (all rows except their current one). */
const moveTargets = computed(() => {
  if (!userContextMenu.value) return []
  const current = userContextMenu.value.currentChannelId
  return rows.value.filter(ch => ch.id !== current)
})

// User profile popup state
const profilePopup = ref<{ user: User; x: number; y: number } | null>(null)

function openProfilePopup(event: MouseEvent, user: User): void {
  event.stopPropagation()
  profilePopup.value = { user, x: event.clientX, y: event.clientY }
}

function closeProfilePopup(): void {
  profilePopup.value = null
}

function handleProfileKick(userId: number): void {
  emit('kickUser', userId)
  closeProfilePopup()
}

// Channel drag-to-reorder (owner only)
const dragChannelId = ref<number | null>(null)
const dragOverChannelId = ref<number | null>(null)

function handleDragStart(e: DragEvent, channelId: number): void {
  if (!props.isOwner) return
  dragChannelId.value = channelId
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(channelId))
  }
}

function handleDragOver(e: DragEvent, channelId: number): void {
  if (!props.isOwner || dragChannelId.value === null) return
  e.preventDefault()
  dragOverChannelId.value = channelId
}

function handleDragLeave(): void {
  dragOverChannelId.value = null
}

function handleDrop(e: DragEvent, targetChannelId: number): void {
  e.preventDefault()
  dragOverChannelId.value = null
  if (!props.isOwner || dragChannelId.value === null) {
    dragChannelId.value = null
    return
  }
  // Reorder is visual-only in client (no server persistence for sort_order yet).
  // Emit a reorder event that could be wired up when the server supports it.
  dragChannelId.value = null
}

function handleDragEnd(): void {
  dragChannelId.value = null
  dragOverChannelId.value = null
}

// Recording
function isChannelRecording(channelId: number): boolean {
  return !!props.recordingChannels[channelId]?.recording
}

async function toggleRecording(channelId: number, event: MouseEvent): Promise<void> {
  event.stopPropagation()
  if (isChannelRecording(channelId)) {
    await StopRecording(channelId)
  } else {
    await StartRecording(channelId)
  }
}
</script>

<template>
  <section class="flex flex-col h-full min-h-0 " @click="closeContextMenu(); closeUserContextMenu()">
    <div class="border-b border-base-content/10 px-2 py-1.5 min-h-11">
      <div class="dropdown dropdown-bottom w-full">
        <div tabindex="0" role="button" class="btn btn-ghost btn-sm w-full justify-between px-2 normal-case">
          <span class="text-xs font-semibold uppercase tracking-widest opacity-60 truncate">
            {{ serverName || 'Server' }}
          </span>
          <ChevronDown class="w-3.5 h-3.5 opacity-50" aria-hidden="true" />
        </div>
        <ul tabindex="0" class="dropdown-content menu menu-sm z-[1] mt-1 w-56 rounded-box border border-base-content/10 bg-base-200 p-1 shadow">
          <li v-if="canCreateChannels">
            <button class="gap-2" @click="openCreateDialog">
              <Plus class="w-4 h-4" aria-hidden="true" />
              Create Channel
            </button>
          </li>
          <li v-if="canOpenServerAdminSettings">
            <button class="gap-2" @click="openServerAdminModal">
              <Settings class="w-4 h-4" aria-hidden="true" />
              Admin Server Settings
            </button>
          </li>
          <li v-if="!canCreateChannels && !canOpenServerAdminSettings">
            <span class="text-xs opacity-50 cursor-default">No server actions available</span>
          </li>
        </ul>
      </div>
    </div>

    <div v-if="connectError" class="mx-2 mt-2 rounded-md bg-error/10 border border-error/30 px-3 py-2 text-xs text-error">
      {{ connectError }}
    </div>

    <ul class="w-full menu menu-sm flex-1 min-h-0 overflow-y-auto px-2 py-1 gap-0.5">
      <li
        v-for="channel in rows"
        :key="channel.id"
        :draggable="isOwner"
        :class="[
          'w-full',
          dragOverChannelId === channel.id ? 'outline outline-1 outline-dashed outline-primary rounded-lg' : '',
          dragChannelId === channel.id ? 'opacity-50' : '',
        ]"
        @click="selectChannel(channel.id)"
        @dragstart="handleDragStart($event, channel.id)"
        @dragover="handleDragOver($event, channel.id)"
        @dragleave="handleDragLeave"
        @drop="handleDrop($event, channel.id)"
        @dragend="handleDragEnd"
      >
        <!-- Channel header -->
        <a
          class="group flex w-full min-w-0 items-center justify-start gap-1.5 text-left"
          :class="[
            selectedChannelId === channel.id ? 'active' : '',
            isConnectedToChannel(channel.id) ? 'font-semibold' : '',
          ]"
          @contextmenu="openContextMenu($event, channel)"
        >
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
              <Check class="w-3 h-3" aria-hidden="true" />
            </button>
          </template>

          <template v-else>
            <!-- Channel icon: speaker if connected, hash otherwise -->
            <Volume2 v-if="isConnectedToChannel(channel.id)" class="w-3.5 h-3.5 shrink-0 text-success" aria-hidden="true" />
            <span v-else class="text-base-content/40 font-bold text-xs shrink-0">#</span>

            <span class="truncate flex-1">{{ channel.name }}</span>

            <!-- Recording indicator -->
            <span
              v-if="isChannelRecording(channel.id)"
              class="badge badge-xs badge-error gap-0.5 animate-pulse"
              title="Recording in progress"
            >
              REC
            </span>

            <!-- Recording toggle (owner only) -->
            <button
              v-if="isOwner"
              class="btn btn-ghost btn-xs p-0 w-4 h-4 transition-opacity"
              :class="isChannelRecording(channel.id) ? 'text-error opacity-100' : 'opacity-0 group-hover:opacity-40 hover:!opacity-100'"
              :title="isChannelRecording(channel.id) ? 'Stop recording' : 'Start recording'"
              @click="toggleRecording(channel.id, $event)"
            >
              <Square v-if="isChannelRecording(channel.id)" class="w-3 h-3" aria-hidden="true" />
              <Circle v-else class="w-3 h-3" aria-hidden="true" />
            </button>

            <span
              v-if="unreadCounts[channel.id]"
              class="badge badge-xs badge-error font-bold min-w-[16px]"
            >
              {{ unreadCounts[channel.id] > 99 ? '99+' : unreadCounts[channel.id] }}
            </span>

            <div
              v-if="!isConnectedToChannel(channel.id) || usersForChannel(channel.id).length > 0"
              class="ml-auto flex shrink-0 items-center justify-end gap-1"
            >
              <span
                v-if="usersForChannel(channel.id).length > 0"
                class="badge badge-ghost badge-xs"
              >
                {{ usersForChannel(channel.id).length }}{{ channel.max_users ? '/' + channel.max_users : '' }}
              </span>

              <!-- Join button (hover reveal, hidden if already in this channel) -->
              <button
                v-if="!isConnectedToChannel(channel.id)"
                class="btn btn-xs btn-primary opacity-0 group-hover:opacity-100 transition-opacity"
                title="Connect to voice"
                @click="joinVoice(channel.id, $event)"
              >
                Join
              </button>
            </div>
          </template>
        </a>

        <!-- Nested vertical user list -->
        <ul v-if="usersForChannel(channel.id).length > 0">
          <li v-for="user in usersForChannel(channel.id)" :key="`${channel.id}-${user.id}`">
            <a
              class="flex items-center gap-2 py-1"
              :class="user.id !== myId ? 'cursor-context-menu' : ''"
              @click.stop="openProfilePopup($event, user)"
              @contextmenu="openUserContextMenu($event, user, channel.id)"
            >
              <span
                class="w-5 h-5 rounded-full border text-[9px] font-mono flex items-center justify-center shrink-0 transition-all duration-150"
                :class="speakingUsers.has(user.id) ? 'bg-success/20 border-success ring-1 ring-success/50' : 'bg-base-300 border-base-content/20'"
              >
                {{ initials(user.username) }}
              </span>
              <span class="text-xs truncate">{{ user.username }}</span>
              <span
                v-if="speakingUsers.has(user.id)"
                class="w-1.5 h-1.5 rounded-full bg-success animate-pulse ml-auto shrink-0"
              />
            </a>
          </li>
        </ul>
      </li>
    </ul>

    <div v-if="voiceConnected" class="border-t border-base-content/10 p-2 shrink-0 space-y-1">
      <p class="px-1 text-[10px] font-semibold uppercase tracking-wider opacity-50">Voice Actions</p>
      <div class="flex items-center gap-1">
        <button
          class="btn btn-ghost btn-sm flex-1 justify-start gap-2"
          :class="videoActive ? 'text-success' : ''"
          :disabled="true"
          title="Video is not available yet"
          @click="emit('video-toggle')"
        >
          <Video class="w-4 h-4" aria-hidden="true" />
          <span class="text-xs">{{ videoActive ? 'Video On' : 'Video' }}</span>
        </button>
        <button
          class="btn btn-ghost btn-sm flex-1 justify-start gap-2"
          :class="screenSharing ? 'text-success' : ''"
          :disabled="true"
          title="Screen sharing is not available yet"
          @click="emit('screen-share-toggle')"
        >
          <Monitor class="w-4 h-4" aria-hidden="true" />
          <span class="text-xs">{{ screenSharing ? 'Sharing On' : 'Share Screen' }}</span>
        </button>
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

    <!-- User context menu (right-click on user: volume for all, move/kick for owner) -->
    <Teleport to="body">
      <div
        v-if="userContextMenu"
        class="fixed z-50 min-w-[180px] rounded-lg border border-base-content/15 bg-base-200 shadow-lg py-1"
        :style="{ left: userContextMenu.x + 'px', top: userContextMenu.y + 'px' }"
        @click.stop
      >
        <div class="px-3 py-1 text-[10px] uppercase tracking-wider opacity-40 select-none">
          {{ userContextMenu.user.username }}
        </div>

        <!-- Per-user volume slider -->
        <div class="px-3 py-1.5">
          <div class="flex items-center justify-between mb-1">
            <span class="text-[10px] opacity-50">Volume</span>
            <span class="text-[10px] font-mono font-medium tabular-nums">{{ userVolume }}%</span>
          </div>
          <input
            type="range"
            v-model.number="userVolume"
            min="0"
            max="200"
            step="5"
            class="range range-xs range-primary w-full"
            @input="handleUserVolumeChange"
          />
        </div>

        <template v-if="isOwner">
          <div class="divider my-0.5 opacity-20"></div>
          <button
            class="w-full text-left px-3 py-1.5 text-xs text-error hover:bg-error/10 transition-colors"
            @click="kickUser"
          >
            Kick
          </button>
          <div class="divider my-0.5 opacity-20"></div>
          <div class="px-3 py-1 text-[10px] uppercase tracking-wider opacity-40 select-none">
            Move to
          </div>
          <button
            v-for="target in moveTargets"
            :key="target.id"
            class="w-full text-left px-3 py-1.5 text-xs hover:bg-base-content/10 transition-colors"
            @click="moveUserToChannel(target.id)"
          >
            {{ target.name }}
          </button>
        </template>
      </div>
    </Teleport>

    <!-- User profile popup -->
    <UserProfilePopup
      v-if="profilePopup"
      :user="profilePopup.user"
      :x="profilePopup.x"
      :y="profilePopup.y"
      :is-owner="isOwner"
      :my-id="myId"
      :owner-user-id="ownerId"
      :user-channels="userChannels"
      :speaking-users="speakingUsers"
      @close="closeProfilePopup"
      @kick="handleProfileKick"
    />

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
          <button class="btn btn-soft btn-primary btn-sm" :disabled="!newChannelName.trim()" @click="confirmCreate">Create</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="cancelCreate">
        <button>close</button>
      </form>
    </dialog>

    <dialog class="modal" :class="{ 'modal-open': showServerAdminModal }">
      <div class="modal-box w-96 max-w-[calc(100vw-2rem)]">
        <h3 class="text-sm font-semibold mb-3">Server Admin Settings</h3>
        <div class="space-y-2">
          <label class="text-[11px] font-medium opacity-70">Server name</label>
          <input
            v-model="serverNameDraft"
            type="text"
            class="input input-sm input-bordered w-full"
            maxlength="50"
            :disabled="!canRenameServer || savingServerName"
            placeholder="Enter server name"
            @keydown.enter.prevent="saveServerName"
          />
          <p v-if="!canRenameServer" class="text-[11px] opacity-50">
            Only the server owner can rename the server.
          </p>
          <p v-if="serverOptionsError" class="text-[11px] text-error">
            {{ serverOptionsError }}
          </p>
        </div>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" @click="closeServerAdminModal">Cancel</button>
          <button
            class="btn btn-soft btn-primary btn-sm"
            :disabled="!canRenameServer || savingServerName || !serverNameDraft.trim() || serverNameDraft.trim() === (serverName || '').trim()"
            @click="saveServerName"
          >
            {{ savingServerName ? 'Saving...' : 'Save Server Name' }}
          </button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="closeServerAdminModal">
        <button>close</button>
      </form>
    </dialog>
  </section>
</template>
