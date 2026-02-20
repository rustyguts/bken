<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, DisconnectVoice, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat, SendChannelChat, GetStartupAddr, GetConfig, SaveConfig, JoinChannel, ConnectVoice } from './config'
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

const connectError = ref('')
const startupAddrHint = ref('')
const currentRoute = ref<AppRoute>('room')
const globalUsername = ref('')

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
  await SendChat(message)
}

async function handleSendChannelChat(channelID: number, message: string): Promise<void> {
  await SendChannelChat(channelID, message)
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

  EventsOn('connection:lost', () => {
    connected.value = false
    voiceConnected.value = false
    startReconnect(
      () => { connected.value = true; voiceConnected.value = true; connectError.value = '' },
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

  EventsOn('chat:message', (data: { username: string; message: string; ts: number; channel_id: number }) => {
    chatMessages.value = [
      ...chatMessages.value,
      { id: ++chatIdCounter, username: data.username, message: data.message, ts: data.ts, channelId: data.channel_id ?? 0 },
    ]
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
  EventsOff('connection:lost', 'user:list', 'user:joined', 'user:left', 'chat:message', 'server:info', 'room:owner', 'user:me', 'connection:kicked', 'channel:list', 'channel:user_moved', 'audio:speaking')
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
          @connect="handleConnect"
          @activate-channel="handleActivateChannel"
          @rename-global-username="handleRenameGlobalUsername"
          @open-settings="openSettingsPage"
          @disconnect="handleDisconnect"
          @disconnect-voice="handleDisconnectVoice"
          @send-chat="handleSendChat"
          @send-channel-chat="handleSendChannelChat"
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
