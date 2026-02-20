<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import ServerChannels from './ServerChannels.vue'
import UserControls from './UserControls.vue'
import ChannelChatroom from './ChannelChatroom.vue'
import type { User, ChatMessage, Channel, ConnectPayload } from './types'

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
}>()

const muted = ref(false)
const deafened = ref(false)
const selectedChannelId = ref(0)
const selectedServerAddr = ref('')

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)

watch(myChannelId, (id) => {
  selectedChannelId.value = id
}, { immediate: true })

watch(() => props.channels, () => {
  if (selectedChannelId.value === 0) return
  if (!props.channels.some(ch => ch.id === selectedChannelId.value)) {
    selectedChannelId.value = 0
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
}

function handleSelectServer(addr: string): void {
  selectedServerAddr.value = addr
  selectedChannelId.value = 0

  // Opening another server from the sidebar should not keep the current server connection active.
  if (props.connected && props.connectedAddr !== addr) {
    emit('disconnect')
  }
}

function handleSendMessage(message: string): void {
  if (selectedChannelId.value === 0) {
    emit('sendChat', message)
    return
  }
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
      :selected-channel-id="selectedChannelId"
      :server-name="serverName"
      :speaking-users="speakingUsers"
      :connect-error="connectError"
      @join="handleJoinChannel"
      @select="handleSelectChannel"
    />

    <ChannelChatroom
      class="room-chatroom"
      :messages="chatMessages"
      :channels="channels"
      :selected-channel-id="selectedChannelId"
      :my-channel-id="myChannelId"
      :connected="connected"
      @select-channel="handleSelectChannel"
      @send="handleSendMessage"
    />

    <UserControls
      class="room-controls border-r border-base-content/10"
      :username="globalUsername"
      :muted="muted"
      :deafened="deafened"
      :connected="connected"
      :voice-connected="voiceConnected"
      @rename-username="emit('renameGlobalUsername', $event)"
      @open-settings="emit('openSettings')"
      @mute-toggle="handleMuteToggle"
      @deafen-toggle="handleDeafenToggle"
      @disconnect="handleDisconnectVoice"
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
