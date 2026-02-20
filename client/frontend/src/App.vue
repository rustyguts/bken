<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, GetAutoLogin } from '../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import ServerBrowser from './ServerBrowser.vue'
import Room from './Room.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import type { LogEvent } from './EventLog.vue'

interface User {
  id: number
  username: string
}

const connected = ref(false)
const serverBrowserRef = ref<InstanceType<typeof ServerBrowser> | null>(null)
const users = ref<User[]>([])
const logEvents = ref<LogEvent[]>([])
const speakingUsers = ref<Set<number>>(new Set())
const speakingTimers = new Map<number, ReturnType<typeof setTimeout>>()
let eventIdCounter = 0

// Reconnect state
const reconnecting = ref(false)
const reconnectAttempt = ref(0)
const reconnectSecondsLeft = ref(0)
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let lastAddr = ''
let lastUsername = ''

const BACKOFF = [1, 2, 4, 8, 16, 30] // seconds

function addEvent(text: string, type: LogEvent['type']) {
  const d = new Date()
  const time = d.toLocaleTimeString('en', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  logEvents.value.push({ id: ++eventIdCounter, time, text, type })
}

function setSpeaking(id: number) {
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

function clearReconnectTimers() {
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

function scheduleReconnect() {
  const delay = BACKOFF[Math.min(reconnectAttempt.value, BACKOFF.length - 1)]
  reconnectSecondsLeft.value = delay

  countdownTimer = setInterval(() => {
    reconnectSecondsLeft.value = Math.max(0, reconnectSecondsLeft.value - 1)
  }, 1000)

  reconnectTimer = setTimeout(async () => {
    clearInterval(countdownTimer!)
    countdownTimer = null
    reconnectAttempt.value++
    addEvent(`Reconnecting… (attempt ${reconnectAttempt.value})`, 'info')

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

EventsOn('user:joined', (data: { id: number; username: string }) => {
  users.value = [...users.value, { id: data.id, username: data.username }]
  addEvent(`${data.username} joined`, 'join')
})

EventsOn('user:left', (data: { id: number }) => {
  const user = users.value.find(u => u.id === data.id)
  users.value = users.value.filter(u => u.id !== data.id)
  addEvent(`${user?.username ?? 'Someone'} left`, 'leave')
})

EventsOn('audio:speaking', (data: { id: number }) => {
  setSpeaking(data.id)
})

onBeforeUnmount(() => {
  EventsOff('connection:lost')
  EventsOff('user:list')
  EventsOff('user:joined')
  EventsOff('user:left')
  EventsOff('audio:speaking')
  clearReconnectTimers()
  speakingTimers.forEach(t => clearTimeout(t))
})

onMounted(async () => {
  const auto = await GetAutoLogin()
  if (auto.username) await handleConnect({ username: auto.username, addr: auto.addr })
})

async function handleConnect(payload: { username: string; addr: string }) {
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

function clearSpeaking() {
  speakingTimers.forEach(t => clearTimeout(t))
  speakingTimers.clear()
  speakingUsers.value = new Set()
}

function handleDisconnect() {
  clearReconnectTimers()
  reconnecting.value = false
  reconnectAttempt.value = 0
  connected.value = false
  users.value = []
  logEvents.value = []
  clearSpeaking()
}

async function handleCancelReconnect() {
  clearReconnectTimers()
  reconnecting.value = false
  reconnectAttempt.value = 0
  connected.value = false
  users.value = []
  logEvents.value = []
  clearSpeaking()
  await Disconnect() // reset Go state in case a reconnect resolved in-flight
}
</script>

<template>
  <main class="h-full">
    <ReconnectBanner
      v-if="reconnecting"
      :attempt="reconnectAttempt"
      :seconds-until-retry="reconnectSecondsLeft"
      @cancel="handleCancelReconnect"
    />
    <Room
      v-if="connected || reconnecting"
      :users="users"
      :speaking-users="speakingUsers"
      :log-events="logEvents"
      @disconnect="handleDisconnect"
    />
    <ServerBrowser v-else ref="serverBrowserRef" @connect="handleConnect" />
  </main>
</template>
