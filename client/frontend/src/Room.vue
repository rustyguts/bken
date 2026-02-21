<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import ServerChannels from './ServerChannels.vue'
import UserControls from './UserControls.vue'
import ChannelChatroom from './ChannelChatroom.vue'
import VideoGrid from './VideoGrid.vue'
import type { User, ChatMessage, Channel, ConnectPayload, VideoState } from './types'

const props = defineProps<{
  connected: boolean
  voiceConnected: boolean
  reconnecting: boolean
  connectedAddr: string
  connectError: string
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
  recordingChannels: Record<number, { recording: boolean; startedBy: string }>
  typingUsers: Record<number, { username: string; channelId: number; expiresAt: number }>
  messageDensity: 'compact' | 'default' | 'comfortable'
  showSystemMessages: boolean
}>()


const emit = defineEmits<{
  connect: [payload: ConnectPayload]
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
    selectedServerAddr.value = addr.startsWith('bken://') ? addr.slice('bken://'.length) : addr
  }
}, { immediate: true })

async function handleMuteToggle(): Promise<void> {
  muted.value = !muted.value
  await SetMuted(muted.value)
}

async function handleDeafenToggle(): Promise<void> {
  deafened.value = !deafened.value
  await SetDeafened(deafened.value)
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
  selectedServerAddr.value = addr
  selectedChannelId.value = 0

  // Opening another server from the sidebar should not keep the current server connection active.
  if (props.connected && props.connectedAddr !== addr) {
    emit('disconnect')
  }
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
</script>

<template>
  <div class="room-grid h-full min-h-0 overflow-hidden">
    <Sidebar
      class="room-sidebar"
      :active-server-addr="selectedServerAddr"
      :connected-addr="connectedAddr"
      :connect-error="connectError"
      :startup-addr="startupAddr"
      :global-username="globalUsername"
      @connect="emit('connect', $event)"
      @select-server="handleSelectServer"
    />

    <ServerChannels
      class="room-channels border-r border-base-content/10 bg-base-100"
      :channels="channels"
      :users="users"
      :user-channels="userChannels"
      :my-id="myId"
      :connected-addr="connectedAddr"
      :selected-channel-id="selectedChannelId"
      :server-name="serverName"
      :speaking-users="speakingUsers"
      :connect-error="connectError"
      :is-owner="isOwner"
      :owner-id="ownerId"
      :unread-counts="unreadCounts"
      :recording-channels="recordingChannels"
      @join="handleJoinChannel"
      @select="handleSelectChannel"
      @create-channel="emit('createChannel', $event)"
      @rename-channel="(id, name) => emit('renameChannel', id, name)"
      @delete-channel="emit('deleteChannel', $event)"
      @move-user="(uid, chid) => emit('moveUser', uid, chid)"
      @kick-user="emit('kickUser', $event)"
    />

    <div class="room-chatroom flex flex-col min-h-0">
      <VideoGrid
        :users="users"
        :video-states="videoStates"
        :my-id="myId"
        :spotlight-id="spotlightId"
        @spotlight="spotlightId = $event"
      />

    <ChannelChatroom
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

    <UserControls
      class="room-controls border-r border-base-content/10"
      :username="globalUsername"
      :muted="muted"
      :deafened="deafened"
      :connected="connected"
      :voice-connected="voiceConnected"
      :video-active="videoActive"
      :screen-sharing="screenSharing"
      @rename-username="emit('renameGlobalUsername', $event)"
      @open-settings="emit('openSettings')"
      @mute-toggle="handleMuteToggle"
      @deafen-toggle="handleDeafenToggle"
      @leave-voice="handleDisconnectVoice"
      @video-toggle="handleVideoToggle"
      @screen-share-toggle="handleScreenShareToggle"
    />
  </div>
</template>

<style scoped>
.room-grid {
  display: grid;
  grid-template-columns: 64px minmax(220px, 280px) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr) auto;
}

.room-sidebar {
  grid-column: 1;
  grid-row: 1;
  min-height: 0;
}

.room-channels {
  grid-column: 2;
  grid-row: 1;
  min-height: 0;
}

.room-chatroom {
  grid-column: 3;
  grid-row: 1 / span 2;
  min-height: 0;
}

.room-controls {
  grid-column: 1 / span 2;
  grid-row: 2;
  min-width: 0;
}
</style>
