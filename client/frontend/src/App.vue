<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat } from './config'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import ServerBrowser from './ServerBrowser.vue'
import Room from './Room.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import TitleBar from './TitleBar.vue'
import type { User, UserJoinedEvent, UserLeftEvent, SpeakingEvent, LogEvent, ConnectPayload, ChatMessage } from './types'

const connected = ref(false)
const serverBrowserRef = ref<InstanceType<typeof ServerBrowser> | null>(null)
const users = ref<User[]>([])
const logEvents = ref<LogEvent[]>([])
const chatMessages = ref<ChatMessage[]>([])
const serverName = ref('')
const ownerID = ref(0)
const myID = ref(0)
const speakingUsers = ref<Set<number>>(new Set())
const speakingTimers = new Map<number, ReturnType<typeof setTimeout>>()
let eventIdCounter = 0
let chatIdCounter = 0

// Reconnect state
const reconnecting = ref(false)
const reconnectAttempt = ref(0)
const reconnectSecondsLeft = ref(0)
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let lastAddr = ''
let lastUsername = ''

/** Exponential backoff delays in seconds. */
const BACKOFF = [1, 2, 4, 8, 16, 30] as const

function addEvent(text: string, type: LogEvent['type']): void {
  const d = new Date()
  const time = d.toLocaleTimeString('en', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  logEvents.value.push({ id: ++eventIdCounter, time, text, type })
}

function setSpeaking(id: number): void {
  const next = new Set(speakingUsers.value)
  next.add(id)
  speakingUsers.value = next
  const existing = speakingTimers.get(id)
  if (existing) clearTimeout(existing)
  speakingTimers.set(id, setTimeout(() => {
    const updated = new Set(speakingUsers.value)
    updated.delete(id)
    speakingUsers.value = updated
    speakingTimers.delete(id)
  }, 500))
}

function clearSpeaking(): void {
  speakingTimers.forEach(t => clearTimeout(t))
  speakingTimers.clear()
  speakingUsers.value = new Set()
}

function clearReconnectTimers(): void {
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

function scheduleReconnect(): void {
  const delay = BACKOFF[Math.min(reconnectAttempt.value, BACKOFF.length - 1)]
  reconnectSecondsLeft.value = delay

  countdownTimer = setInterval(() => {
    reconnectSecondsLeft.value = Math.max(0, reconnectSecondsLeft.value - 1)
  }, 1000)

  reconnectTimer = setTimeout(async () => {
    clearInterval(countdownTimer!)
    countdownTimer = null
    reconnectAttempt.value++
    addEvent(`Reconnecting... (attempt ${reconnectAttempt.value})`, 'info')

    const err = await Connect(lastAddr, lastUsername)
    if (!err) {
      reconnecting.value = false
      reconnectAttempt.value = 0
      connected.value = true
      addEvent('Reconnected', 'join')
    } else {
      scheduleReconnect()
    }
  }, delay * 1000)
}

function resetState(): void {
  connected.value = false
  users.value = []
  logEvents.value = []
  chatMessages.value = []
  serverName.value = ''
  ownerID.value = 0
  myID.value = 0
  clearSpeaking()
}

async function handleConnect(payload: ConnectPayload): Promise<void> {
  lastAddr = payload.addr
  lastUsername = payload.username
  users.value = []
  logEvents.value = []
  const err = await Connect(payload.addr, payload.username)
  if (err) {
    serverBrowserRef.value?.setError(err)
  } else {
    connected.value = true
    addEvent('Connected', 'info')
  }
}

async function handleSendChat(message: string): Promise<void> {
  await SendChat(message)
}

async function handleDisconnect(): Promise<void> {
  clearReconnectTimers()
  reconnecting.value = false
  reconnectAttempt.value = 0
  resetState()
  await Disconnect()
}

async function handleCancelReconnect(): Promise<void> {
  clearReconnectTimers()
  reconnecting.value = false
  reconnectAttempt.value = 0
  resetState()
  await Disconnect()
}

onMounted(async () => {
  EventsOn('connection:lost', () => {
    if (reconnecting.value) return
    clearSpeaking()
    reconnecting.value = true
    addEvent('Connection lost', 'leave')
    scheduleReconnect()
  })

  EventsOn('user:list', (data: User[]) => {
    users.value = data || []
    if (data?.length) addEvent(`${data.length} user${data.length !== 1 ? 's' : ''} in room`, 'info')
  })

  EventsOn('user:joined', (data: UserJoinedEvent) => {
    users.value = [...users.value, { id: data.id, username: data.username }]
    addEvent(`${data.username} joined`, 'join')
  })

  EventsOn('user:left', (data: UserLeftEvent) => {
    const user = users.value.find(u => u.id === data.id)
    users.value = users.value.filter(u => u.id !== data.id)
    addEvent(`${user?.username ?? 'Someone'} left`, 'leave')
  })

  EventsOn('audio:speaking', (data: SpeakingEvent) => {
    setSpeaking(data.id)
  })

  EventsOn('chat:message', (data: { username: string; message: string; ts: number }) => {
    chatMessages.value = [
      ...chatMessages.value,
      { id: ++chatIdCounter, username: data.username, message: data.message, ts: data.ts },
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

  EventsOn('connection:kicked', () => {
    addEvent('You were kicked from the server', 'leave')
    clearReconnectTimers()
    reconnecting.value = false
    reconnectAttempt.value = 0
    resetState()
  })

  // Apply saved audio settings before doing anything else so noise suppression,
  // AGC, and volume are active even if the user never opens the settings panel.
  await ApplyConfig()

  // Auto-login if configured
  const auto = await GetAutoLogin()
  if (auto.username) await handleConnect({ username: auto.username, addr: auto.addr })
})

onBeforeUnmount(() => {
  EventsOff('connection:lost', 'user:list', 'user:joined', 'user:left', 'audio:speaking', 'chat:message', 'server:info', 'room:owner', 'user:me', 'connection:kicked')
  clearReconnectTimers()
  speakingTimers.forEach(t => clearTimeout(t))
})
</script>

<template>
  <main class="flex flex-col h-full">
    <TitleBar :server-name="serverName" />
    <Transition name="slide-down">
      <ReconnectBanner
        v-if="reconnecting"
        :attempt="reconnectAttempt"
        :seconds-until-retry="reconnectSecondsLeft"
        @cancel="handleCancelReconnect"
      />
    </Transition>
    <Transition name="fade" mode="out-in">
      <Room
        v-if="connected || reconnecting"
        key="room"
        :users="users"
        :speaking-users="speakingUsers"
        :log-events="logEvents"
        :chat-messages="chatMessages"
        :owner-id="ownerID"
        :my-id="myID"
        class="flex-1 min-h-0"
        @disconnect="handleDisconnect"
        @send-chat="handleSendChat"
      />
      <ServerBrowser v-else key="browser" ref="serverBrowserRef" class="flex-1 min-h-0" @connect="handleConnect" />
    </Transition>
  </main>
</template>
