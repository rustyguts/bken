<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import ServerChannels from './ServerChannels.vue'
import ChannelChat from './ChannelChat.vue'
import VideoGrid from './VideoGrid.vue'
import WelcomePage from './WelcomePage.vue'
import { BKEN_SCHEME } from './constants'
import { usePanelWidth } from './composables/usePanelWidth'
import type { ServerEntry } from './config'
import type { User, ChatMessage, Channel, ConnectPayload, VideoState } from './types'

const props = defineProps<{
  connected: boolean
  voiceConnected: boolean
  reconnecting: boolean
  connectedAddr: string
  startupAddr: string
  globalUsername: string
  serverName: string
  users: User[]
  chatMessages: ChatMessage[]
  ownerId: number
  myId: number
  channels: Channel[]
  userChannels: Record<number, number>
  speakingUsers: Set<number>
  unreadCounts: Record<number, number>
  videoStates: Record<number, VideoState>
  typingUsers: Record<number, { username: string; channelId: number; expiresAt: number }>
  messageDensity: 'compact' | 'default' | 'comfortable'
  showSystemMessages: boolean
  servers: ServerEntry[]
  userVoiceFlags: Record<number, { muted: boolean; deafened: boolean }>
}>()


const emit = defineEmits<{
  connect: [payload: ConnectPayload]
  selectServer: [addr: string]
  activateChannel: [payload: { addr: string; channelID: number }]
  renameGlobalUsername: [username: string]
  openSettings: []
  disconnect: []
  disconnectVoice: []
  sendChat: [message: string]
  sendChannelChat: [channelID: number, message: string]
  createChannel: [name: string]
  renameChannel: [channelID: number, name: string]
  deleteChannel: [channelID: number]
  moveUser: [userID: number, channelID: number]
  kickUser: [userID: number]
  uploadFile: [channelID: number]
  uploadFileFromPath: [channelID: number, path: string]
  viewChannel: [channelID: number]
  editMessage: [msgID: number, message: string]
  deleteMessage: [msgID: number]
  addReaction: [msgID: number, emoji: string]
  removeReaction: [msgID: number, emoji: string]
  startVideo: []
  stopVideo: []
  startScreenShare: []
  stopScreenShare: []
}>()

const { gridCols, setPanelWidth } = usePanelWidth()

const muted = ref(false)
const deafened = ref(false)
const selectedChannelId = ref(0)
const selectedServerAddr = ref('')
const spotlightId = ref<number | null>(null)

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)
const isOwner = computed(() => props.ownerId !== 0 && props.ownerId === props.myId)
const myVideoState = computed(() => props.videoStates[props.myId])
const videoActive = computed(() => myVideoState.value?.active === true && !myVideoState.value?.screenShare)
const screenSharing = computed(() => myVideoState.value?.active === true && myVideoState.value?.screenShare === true)

watch(myChannelId, (id) => {
  if (id <= 0) return
  selectedChannelId.value = id
  emit('viewChannel', id)
}, { immediate: true })

watch(() => props.channels, () => {
  if (!props.channels.some(ch => ch.id === selectedChannelId.value)) {
    const fallback = props.channels.length > 0 ? props.channels[0].id : 0
    selectedChannelId.value = fallback
    emit('viewChannel', fallback)
  }
})

watch(() => props.connectedAddr, (addr) => {
  if (addr) {
    selectedServerAddr.value = addr
  }
}, { immediate: true })

watch(() => props.startupAddr, (addr) => {
  if (!selectedServerAddr.value && addr) {
    selectedServerAddr.value = addr.startsWith(BKEN_SCHEME) ? addr.slice(BKEN_SCHEME.length) : addr
  }
}, { immediate: true })

async function handleMuteToggle(): Promise<void> {
  if (muted.value) {
    // Unmuting: if deafened, also undeafen
    muted.value = false
    if (deafened.value) {
      deafened.value = false
      await SetDeafened(false)
    }
    await SetMuted(false)
  } else {
    muted.value = true
    await SetMuted(true)
  }
}

async function handleDeafenToggle(): Promise<void> {
  if (deafened.value) {
    // Undeafening: just undeafen (mute stays as-is)
    deafened.value = false
    await SetDeafened(false)
  } else {
    // Deafening: also mute
    deafened.value = true
    if (!muted.value) {
      muted.value = true
      await SetMuted(true)
    }
    await SetDeafened(true)
  }
}

function handleDisconnectVoice(): void {
  emit('disconnectVoice')
}

async function handleJoinChannel(channelID: number): Promise<void> {
  const targetAddr = selectedServerAddr.value || props.connectedAddr
  if (!targetAddr) return
  emit('activateChannel', { addr: targetAddr, channelID })
}

function handleSelectChannel(channelID: number): void {
  selectedChannelId.value = channelID
  emit('viewChannel', channelID)
}

function handleSelectServer(addr: string): void {
  emit('selectServer', addr)
}

function handleVideoToggle(): void {
  if (videoActive.value) {
    emit('stopVideo')
  } else {
    emit('startVideo')
  }
}

function handleScreenShareToggle(): void {
  if (screenSharing.value) {
    emit('stopScreenShare')
  } else {
    emit('startScreenShare')
  }
}

// Keyboard shortcut handlers
function onShortcutMute(): void {
  if (props.voiceConnected) handleMuteToggle()
}
function onShortcutDeafen(): void {
  if (props.voiceConnected) handleDeafenToggle()
}

onMounted(() => {
  window.addEventListener('shortcut:mute-toggle', onShortcutMute as EventListener)
  window.addEventListener('shortcut:deafen-toggle', onShortcutDeafen as EventListener)
})

onBeforeUnmount(() => {
  window.removeEventListener('shortcut:mute-toggle', onShortcutMute as EventListener)
  window.removeEventListener('shortcut:deafen-toggle', onShortcutDeafen as EventListener)
})

function handleSendMessage(message: string): void {
  emit('sendChannelChat', selectedChannelId.value, message)
}

// --- Panel resize ---
const SIDEBAR_WIDTH = 64
const resizing = ref(false)

function onResizeStart(e: MouseEvent): void {
  e.preventDefault()
  resizing.value = true
  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
}

function onResizeMove(e: MouseEvent): void {
  setPanelWidth(e.clientX - SIDEBAR_WIDTH)
}

function onResizeEnd(): void {
  resizing.value = false
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
}
</script>

<template>
  <div class="grid grid-rows-[minmax(0,1fr)] h-full min-h-0" :style="{ gridTemplateColumns: gridCols }">
    <Sidebar
      class="col-start-1 row-start-1 min-h-0"
      :active-server-addr="selectedServerAddr"
      :connected-addr="connectedAddr"
      :connected="connected"
      :voice-connected="voiceConnected"
      :startup-addr="startupAddr"
      :global-username="globalUsername"
      @select-server="handleSelectServer"
      @go-home="emit('disconnect')"
      @rename-username="emit('renameGlobalUsername', $event)"
      @open-settings="emit('openSettings')"
    />

    <template v-if="connected">
      <ServerChannels
        class="col-start-2 row-start-1 min-h-0 bg-base-100"
        :channels="channels"
        :users="users"
        :user-channels="userChannels"
        :my-id="myId"
        :connected-addr="connectedAddr"
        :selected-channel-id="selectedChannelId"
        :server-name="serverName"
        :speaking-users="speakingUsers"
        :voice-connected="voiceConnected"
        :video-active="videoActive"
        :screen-sharing="screenSharing"
        :is-owner="isOwner"
        :owner-id="ownerId"
        :unread-counts="unreadCounts"
        :muted="muted"
        :deafened="deafened"
        :user-voice-flags="userVoiceFlags"
        @join="handleJoinChannel"
        @select="handleSelectChannel"
        @create-channel="emit('createChannel', $event)"
        @rename-channel="(id, name) => emit('renameChannel', id, name)"
        @delete-channel="emit('deleteChannel', $event)"
        @move-user="(uid, chid) => emit('moveUser', uid, chid)"
        @kick-user="emit('kickUser', $event)"
        @video-toggle="handleVideoToggle"
        @screen-share-toggle="handleScreenShareToggle"
        @leave-voice="handleDisconnectVoice"
        @mute-toggle="handleMuteToggle"
        @deafen-toggle="handleDeafenToggle"
      />

      <!-- Resize handle between columns 2 and 3 -->
      <div
        class="col-start-2 row-start-1 w-1 justify-self-end z-10 cursor-col-resize transition-colors hover:bg-primary/30"
        :class="{ 'bg-primary/30': resizing }"
        @mousedown="onResizeStart"
      />

      <div class="col-start-3 row-start-1 min-h-0 flex flex-col">
        <VideoGrid
          :users="users"
          :video-states="videoStates"
          :my-id="myId"
          :spotlight-id="spotlightId"
          @spotlight="spotlightId = $event"
        />

        <ChannelChat
          class="flex-1 min-h-0"
          :messages="chatMessages"
          :channels="channels"
          :selected-channel-id="selectedChannelId"
          :my-channel-id="myChannelId"
          :connected="connected"
          :unread-counts="unreadCounts"
          :my-id="myId"
          :owner-id="ownerId"
          :users="users"
          :typing-users="typingUsers"
          :message-density="messageDensity"
          :show-system-messages="showSystemMessages"
          @select-channel="handleSelectChannel"
          @send="handleSendMessage"
          @upload-file="emit('uploadFile', selectedChannelId)"
          @upload-file-from-path="(path: string) => emit('uploadFileFromPath', selectedChannelId, path)"
          @edit-message="(msgID: number, message: string) => emit('editMessage', msgID, message)"
          @delete-message="(msgID: number) => emit('deleteMessage', msgID)"
          @add-reaction="(msgID: number, emoji: string) => emit('addReaction', msgID, emoji)"
          @remove-reaction="(msgID: number, emoji: string) => emit('removeReaction', msgID, emoji)"
        />
      </div>
    </template>

    <WelcomePage
      v-else
      class="col-[2/span_2] row-start-1 min-h-0"
      :servers="servers"
      :global-username="globalUsername"
      :startup-addr="startupAddr"
      @connect="emit('connect', $event)"
    />
  </div>
</template>

