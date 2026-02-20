<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, DisconnectVoice, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat, SendChannelChat, GetStartupAddr, GetConfig, SaveConfig, JoinChannel, ConnectVoice, CreateChannel, RenameChannel, DeleteChannel, MoveUserToChannel, UploadFile, UploadFileFromPath } from './config'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import Room from './Room.vue'
import SettingsPage from './SettingsPage.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import TitleBar from './TitleBar.vue'
import { useReconnect } from './composables/useReconnect'
import { useSpeakingUsers } from './composables/useSpeakingUsers'
import type { User, UserJoinedEvent, UserLeftEvent, ConnectPayload, ChatMessage, Channel, SpeakingEvent } from './types'

type AppRoute = 'room' | 'settings'

const connected = ref(false)
const voiceConnected = ref(false)
const users = ref<User[]>([])
const chatMessages = ref<ChatMessage[]>([])
const serverName = ref('')
const ownerID = ref(0)
const myID = ref(0)
const channels = ref<Channel[]>([])
/** Maps userID -> channelID (0 = lobby). Updated by user:list, user:joined, user:left, channel:user_moved. */
const userChannels = ref<Record<number, number>>({})
let chatIdCounter = 0

const activeChannelId = ref(0) // tracks the currently-viewed chatroom channel (for file drops)
const viewedChannelId = ref(0) // tracks which channel's chat the user is currently viewing
const unreadCounts = ref<Record<number, number>>({}) // channelId -> unread message count
const connectError = ref('')
const startupAddrHint = ref('')
const currentRoute = ref<AppRoute>('room')
const globalUsername = ref('')
const joiningVoice = ref(false)

const { reconnecting, reconnectAttempt, reconnectSecondsLeft, startReconnect, cancelReconnect, clearTimers, setLastCredentials } = useReconnect()
const { speakingUsers, setSpeaking, clearSpeaking, cleanup: cleanupSpeaking } = useSpeakingUsers()

/** The WebTransport address the client is currently connected to. Exposed to TitleBar so the owner can generate an invite link. */
const connectedAddr = ref('')

function parseRoute(hash: string): AppRoute {
  return hash === '#/settings' ? 'settings' : 'room'
}

function syncRouteFromHash(): void {
  currentRoute.value = parseRoute(window.location.hash)
}

function goToRoute(route: AppRoute): void {
  const hash = route === 'settings' ? '#/settings' : '#/'
  if (window.location.hash !== hash) {
    window.location.hash = hash
    return
  }
  currentRoute.value = route
}

function openSettingsPage(): void {
  goToRoute('settings')
}

function closeSettingsPage(): void {
  goToRoute('room')
}

function resetState(): void {
  connected.value = false
  voiceConnected.value = false
  connectedAddr.value = ''
  users.value = []
  chatMessages.value = []
  serverName.value = ''
  ownerID.value = 0
  myID.value = 0
  channels.value = []
  userChannels.value = {}
  viewedChannelId.value = 0
  unreadCounts.value = {}
}

function normaliseUsername(name: string): string {
  return name.trim()
}

function normaliseAddr(addr: string): string {
  const cleaned = addr.trim()
  return cleaned.startsWith('bken://') ? cleaned.slice('bken://'.length) : cleaned
}

function hostFromAddr(addr: string): string {
  const clean = normaliseAddr(addr)
  if (!clean) return ''
  if (clean.startsWith('[')) {
    const end = clean.indexOf(']')
    if (end > 1) return clean.slice(1, end)
  }
  const firstColon = clean.indexOf(':')
  return firstColon === -1 ? clean : clean.slice(0, firstColon)
}

function isLocalDevAddr(addr: string): boolean {
  const host = hostFromAddr(addr).toLowerCase()
  return host === 'localhost' || host === '127.0.0.1' || host === '::1' || host === '0.0.0.0'
}

async function connectToServer(addr: string, username: string): Promise<boolean> {
  const targetAddr = normaliseAddr(addr)
  const user = normaliseUsername(username)
  if (!targetAddr) {
    connectError.value = 'Server address is required.'
    return false
  }
  if (!user) {
    connectError.value = 'Set a global username first (right-click your name in User Controls).'
    return false
  }

  if (connected.value && connectedAddr.value === targetAddr) {
    return true
  }

  if (connected.value && connectedAddr.value !== targetAddr) {
    await Disconnect()
    resetState()
  }

  setLastCredentials(targetAddr, user)
  connectError.value = ''

  const err = await Connect(targetAddr, user)
  if (err && err !== 'already connected') {
    connectError.value = err
    return false
  }

  connected.value = true
  voiceConnected.value = true
  connectedAddr.value = targetAddr
  connectError.value = ''
  startupAddrHint.value = ''
  return true
}

async function handleConnect(payload: ConnectPayload): Promise<void> {
  await connectToServer(payload.addr, payload.username)
}

async function handleActivateChannel(payload: { addr: string; channelID: number }): Promise<void> {
  if (joiningVoice.value) return
  joiningVoice.value = true
  try {
    const ok = await connectToServer(payload.addr, globalUsername.value)
    if (!ok) return

    // If voice audio was stopped (via DisconnectVoice), restart it.
    if (!voiceConnected.value) {
      const err = await ConnectVoice(payload.channelID)
      if (err) {
        connectError.value = err
        return
      }
      voiceConnected.value = true
      connectError.value = ''
      return
    }

    const err = await JoinChannel(payload.channelID)
    if (err) {
      connectError.value = err
      return
    }
    connectError.value = ''
  } finally {
    joiningVoice.value = false
  }
}

async function handleRenameGlobalUsername(name: string): Promise<void> {
  const next = normaliseUsername(name)
  if (!next) {
    connectError.value = 'Username cannot be empty.'
    return
  }

  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, username: next })
  globalUsername.value = next

  if (connectError.value.includes('username')) {
    connectError.value = ''
  }
}

async function handleSendChat(message: string): Promise<void> {
  activeChannelId.value = 0
  await SendChat(message)
}

async function handleSendChannelChat(channelID: number, message: string): Promise<void> {
  activeChannelId.value = channelID
  await SendChannelChat(channelID, message)
}

async function handleCreateChannel(name: string): Promise<void> {
  await CreateChannel(name)
}

async function handleRenameChannel(channelID: number, name: string): Promise<void> {
  await RenameChannel(channelID, name)
}

async function handleDeleteChannel(channelID: number): Promise<void> {
  await DeleteChannel(channelID)
}

async function handleMoveUser(userID: number, channelID: number): Promise<void> {
  await MoveUserToChannel(userID, channelID)
}

function handleViewChannel(channelID: number): void {
  viewedChannelId.value = channelID
  if (unreadCounts.value[channelID]) {
    const { [channelID]: _, ...rest } = unreadCounts.value
    unreadCounts.value = rest
  }
}

async function handleUploadFile(channelID: number): Promise<void> {
  activeChannelId.value = channelID
  const err = await UploadFile(channelID)
  if (err) {
    connectError.value = err
  }
}

async function handleUploadFileFromPath(channelID: number, path: string): Promise<void> {
  activeChannelId.value = channelID
  const err = await UploadFileFromPath(channelID, path)
  if (err) {
    connectError.value = err
  }
}

async function handleDisconnectVoice(): Promise<void> {
  const err = await DisconnectVoice()
  if (err) {
    connectError.value = err
    return
  }
  voiceConnected.value = false
  clearSpeaking()
}

async function handleDisconnect(): Promise<void> {
  clearTimers()
  cancelReconnect()
  connectError.value = ''
  closeSettingsPage()
  clearSpeaking()
  resetState()
  await Disconnect()
}

async function handleCancelReconnect(): Promise<void> {
  cancelReconnect()
  connectError.value = ''
  closeSettingsPage()
  resetState()
  await Disconnect()
}

onMounted(async () => {
  syncRouteFromHash()
  window.addEventListener('hashchange', syncRouteFromHash)

  EventsOn('connection:lost', (data: { reason: string } | null) => {
    const reason = data?.reason || 'Connection lost'
    // Remember the voice channel so we can rejoin after reconnect.
    const lastChannel = userChannels.value[myID.value] ?? 0
    connected.value = false
    voiceConnected.value = false
    connectError.value = reason
    startReconnect(
      async () => {
        connected.value = true
        voiceConnected.value = true
        connectError.value = ''
        // Rejoin the voice channel the user was in before the disconnect.
        if (lastChannel > 0) {
          await JoinChannel(lastChannel)
        }
      },
      () => {},
    )
  })

  EventsOn('user:list', (data: User[]) => {
    users.value = data || []
    const map: Record<number, number> = {}
    for (const u of (data || [])) map[u.id] = u.channel_id ?? 0
    userChannels.value = map
  })

  EventsOn('user:joined', (data: UserJoinedEvent) => {
    users.value = [...users.value, { id: data.id, username: data.username }]
    userChannels.value = { ...userChannels.value, [data.id]: 0 }
  })

  EventsOn('user:left', (data: UserLeftEvent) => {
    users.value = users.value.filter(u => u.id !== data.id)
    const { [data.id]: _, ...rest } = userChannels.value
    userChannels.value = rest
  })

  EventsOn('channel:list', (data: Channel[]) => {
    channels.value = data || []
  })

  EventsOn('channel:user_moved', (data: { user_id: number; channel_id: number }) => {
    userChannels.value = { ...userChannels.value, [data.user_id]: data.channel_id }
  })

  EventsOn('chat:message', (data: { username: string; message: string; ts: number; channel_id: number; msg_id: number; file_id?: number; file_name?: string; file_size?: number; file_url?: string }) => {
    const channelId = data.channel_id ?? 0
    chatMessages.value = [
      ...chatMessages.value,
      {
        id: ++chatIdCounter,
        msgId: data.msg_id ?? 0,
        username: data.username,
        message: data.message,
        ts: data.ts,
        channelId,
        fileId: data.file_id,
        fileName: data.file_name,
        fileSize: data.file_size,
        fileUrl: data.file_url,
      },
    ]
    // Increment unread count if the message is in a channel the user is not viewing.
    if (channelId !== viewedChannelId.value) {
      unreadCounts.value = { ...unreadCounts.value, [channelId]: (unreadCounts.value[channelId] ?? 0) + 1 }
    }
  })

  EventsOn('chat:link_preview', (data: { msg_id: number; channel_id: number; url: string; title: string; description: string; image: string; site_name: string }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    updated[idx] = {
      ...updated[idx],
      linkPreview: {
        url: data.url,
        title: data.title,
        description: data.description,
        image: data.image,
        siteName: data.site_name,
      },
    }
    chatMessages.value = updated
  })

  EventsOn('server:info', (data: { name: string }) => {
    serverName.value = data.name
  })

  EventsOn('room:owner', (data: { owner_id: number }) => {
    ownerID.value = data.owner_id
  })

  EventsOn('user:me', (data: { id: number }) => {
    myID.value = data.id
  })

  EventsOn('audio:speaking', (data: SpeakingEvent) => {
    setSpeaking(data.id)
  })

  EventsOn('connection:kicked', () => {
    connectError.value = 'Disconnected by server owner'
    cancelReconnect()
    closeSettingsPage()
    resetState()
  })

  EventsOn('file:dropped', async (data: { paths: string[] }) => {
    if (!connected.value || !data.paths?.length) return
    for (const path of data.paths) {
      const err = await UploadFileFromPath(activeChannelId.value, path)
      if (err) {
        connectError.value = err
        break
      }
    }
  })

  // Apply saved audio settings before doing anything else so noise suppression,
  // AGC, and volume are active even if the user never opens the settings panel.
  await ApplyConfig()

  const cfg = await GetConfig()
  globalUsername.value = cfg.username?.trim() ?? ''

  // Generate a default username if none is configured.
  if (!globalUsername.value) {
    const hex = Array.from(crypto.getRandomValues(new Uint8Array(2)))
      .map(b => b.toString(16).padStart(2, '0'))
      .join('')
    const generated = `User-${hex}`
    await SaveConfig({ ...cfg, username: generated })
    globalUsername.value = generated
  }

  // Priority: env-var auto-login > bken:// invite link > sidebar server browser.
  const [auto, startupAddr] = await Promise.all([GetAutoLogin(), GetStartupAddr()])
  if (auto.username) {
    globalUsername.value = auto.username
  }

  if (auto.username && !isLocalDevAddr(auto.addr)) {
    await connectToServer(auto.addr, auto.username)
  } else if (startupAddr) {
    if (globalUsername.value) {
      // Saved username + invite link -- connect immediately.
      await connectToServer(startupAddr, globalUsername.value)
    } else {
      startupAddrHint.value = startupAddr
      if (!cfg.servers?.some(s => s.addr === startupAddr)) {
        await SaveConfig({
          ...cfg,
          servers: [...(cfg.servers ?? []), { name: 'Invited Server', addr: startupAddr }],
        })
      }
    }
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncRouteFromHash)
  EventsOff('connection:lost', 'user:list', 'user:joined', 'user:left', 'chat:message', 'chat:link_preview', 'server:info', 'room:owner', 'user:me', 'connection:kicked', 'channel:list', 'channel:user_moved', 'audio:speaking', 'file:dropped')
  clearTimers()
  cleanupSpeaking()
})
</script>

<template>
  <main class="app-grid h-full">
    <TitleBar class="app-title" :server-name="serverName" :is-owner="ownerID !== 0 && ownerID === myID" :server-addr="connectedAddr" />

    <div class="app-banner">
      <Transition name="slide-down">
        <ReconnectBanner
          v-if="reconnecting"
          :attempt="reconnectAttempt"
          :seconds-until-retry="reconnectSecondsLeft"
          :reason="connectError"
          @cancel="handleCancelReconnect"
        />
      </Transition>
    </div>

    <div class="app-content min-h-0">
      <Transition name="fade" mode="out-in">
        <SettingsPage
          v-if="currentRoute === 'settings'"
          key="settings"
          class="h-full min-h-0"
          @back="closeSettingsPage"
        />

        <Room
          v-else
          key="room"
          class="h-full min-h-0"
          :connected="connected"
          :voice-connected="voiceConnected"
          :reconnecting="reconnecting"
          :connected-addr="connectedAddr"
          :connect-error="connectError"
          :startup-addr="startupAddrHint"
          :global-username="globalUsername"
          :server-name="serverName"
          :users="users"
          :chat-messages="chatMessages"
          :owner-id="ownerID"
          :my-id="myID"
          :channels="channels"
          :user-channels="userChannels"
          :speaking-users="speakingUsers"
          :unread-counts="unreadCounts"
          @connect="handleConnect"
          @activate-channel="handleActivateChannel"
          @rename-global-username="handleRenameGlobalUsername"
          @open-settings="openSettingsPage"
          @disconnect="handleDisconnect"
          @disconnect-voice="handleDisconnectVoice"
          @send-chat="handleSendChat"
          @send-channel-chat="handleSendChannelChat"
          @create-channel="handleCreateChannel"
          @rename-channel="handleRenameChannel"
          @delete-channel="handleDeleteChannel"
          @move-user="handleMoveUser"
          @upload-file="handleUploadFile"
          @upload-file-from-path="handleUploadFileFromPath"
          @view-channel="handleViewChannel"
        />
      </Transition>
    </div>
  </main>
</template>

<style scoped>
.app-grid {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
}

.app-title {
  grid-row: 1;
}

.app-banner {
  grid-row: 2;
}

.app-content {
  grid-row: 3;
}
</style>
