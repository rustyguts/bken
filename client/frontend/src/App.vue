<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { Connect, DisconnectVoice, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat, SendChannelChat, GetStartupAddr, GetConfig, SaveConfig, JoinChannel, ConnectVoice, CreateChannel, RenameChannel, DeleteChannel, MoveUserToChannel, KickUser, UploadFile, UploadFileFromPath, PTTKeyDown, PTTKeyUp, RenameUser, EditMessage, DeleteMessage, AddReaction, RemoveReaction, SendTyping, StartVideo, StopVideo, StartScreenShare, StopScreenShare, SetActiveServer, DisconnectServer } from './config'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import Room from './Room.vue'
import SettingsPage from './SettingsPage.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import TitleBar from './TitleBar.vue'
import KeyboardShortcuts from './KeyboardShortcuts.vue'
import { useSpeakingUsers } from './composables/useSpeakingUsers'
import { BKEN_SCHEME, LAST_CONNECTED_ADDR_KEY } from './constants'
import type { User, ConnectPayload, ChatMessage, Channel, VideoState, ReactionInfo } from './types'

type AppRoute = 'room' | 'settings'

interface ServerState {
  connected: boolean
  users: User[]
  chatMessages: ChatMessage[]
  serverName: string
  ownerID: number
  myID: number
  channels: Channel[]
  userChannels: Record<number, number>
  viewedChannelId: number
  unreadCounts: Record<number, number>
  videoStates: Record<number, VideoState>
  recordingChannels: Record<number, { recording: boolean; startedBy: string }>
  typingUsers: Record<number, { username: string; channelId: number; expiresAt: number }>
  connectError: string
}

const reconnecting = ref(false)
const reconnectAttempt = ref(0)
const reconnectSecondsLeft = ref(0)

const startupAddrHint = ref('')
const currentRoute = ref<AppRoute>('room')
const globalUsername = ref('')
const joiningVoice = ref(false)
const disconnectingVoice = ref(false)
const showShortcutsHelp = ref(false)
const messageDensity = ref<'compact' | 'default' | 'comfortable'>('default')
const showSystemMessages = ref(true)
const voiceConnected = ref(false)
const voiceServerAddr = ref('')
const activeChannelId = ref(0)

const activeServerAddr = ref('')
const serverStates = ref<Record<string, ServerState>>({})
const connectedServers = ref<string[]>([])
let chatIdCounter = 0
let typingCleanupInterval: ReturnType<typeof setInterval> | null = null

const { speakingUsers, setSpeaking, clearSpeaking, cleanup: cleanupSpeaking } = useSpeakingUsers()

// Push-to-Talk state
const pttEnabled = ref(false)
const pttKeyCode = ref('Backquote')
let pttKeyHeld = false

function emptyServerState(): ServerState {
  return {
    connected: false,
    users: [],
    chatMessages: [],
    serverName: '',
    ownerID: 0,
    myID: 0,
    channels: [],
    userChannels: {},
    viewedChannelId: 0,
    unreadCounts: {},
    videoStates: {},
    recordingChannels: {},
    typingUsers: {},
    connectError: '',
  }
}

function normaliseUsername(name: string): string {
  return name.trim()
}

function normaliseAddr(addr: string): string {
  const cleaned = addr.trim()
  return cleaned.startsWith(BKEN_SCHEME) ? cleaned.slice(BKEN_SCHEME.length) : cleaned
}

function ensureServer(addr: string): ServerState {
  const key = normaliseAddr(addr)
  if (!key) return emptyServerState()
  if (!serverStates.value[key]) {
    serverStates.value = { ...serverStates.value, [key]: emptyServerState() }
  }
  return serverStates.value[key]
}

function withServer(addr: string, updater: (state: ServerState) => void): void {
  const key = normaliseAddr(addr)
  if (!key) return
  const state = ensureServer(key)
  updater(state)
  serverStates.value = { ...serverStates.value, [key]: { ...state } }
}

function eventServerAddr(data: any): string {
  const raw = typeof data === 'object' && data && 'server_addr' in data ? String(data.server_addr || '') : activeServerAddr.value
  return normaliseAddr(raw)
}

function markConnected(addr: string, connected: boolean): void {
  const key = normaliseAddr(addr)
  if (!key) return
  withServer(key, state => {
    state.connected = connected
    if (!connected) state.connectError = state.connectError || 'Disconnected'
  })
  const next = new Set(connectedServers.value)
  if (connected) next.add(key)
  else next.delete(key)
  connectedServers.value = [...next]
}

const activeState = computed(() => {
  const key = normaliseAddr(activeServerAddr.value)
  if (!key) return emptyServerState()
  return serverStates.value[key] ?? emptyServerState()
})

const connected = computed(() => activeState.value.connected)
const connectedAddr = computed(() => normaliseAddr(activeServerAddr.value))
const users = computed(() => activeState.value.users)
const chatMessages = computed(() => activeState.value.chatMessages)
const serverName = computed(() => activeState.value.serverName)
const ownerID = computed(() => activeState.value.ownerID)
const myID = computed(() => activeState.value.myID)
const channels = computed(() => activeState.value.channels)
const userChannels = computed(() => activeState.value.userChannels)
const unreadCounts = computed(() => activeState.value.unreadCounts)
const videoStates = computed(() => activeState.value.videoStates)
const recordingChannels = computed(() => activeState.value.recordingChannels)
const typingUsers = computed(() => activeState.value.typingUsers)
const connectError = computed(() => activeState.value.connectError)

function setActiveError(message: string): void {
  const addr = connectedAddr.value
  if (!addr) return
  withServer(addr, state => {
    state.connectError = message
  })
}

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

function isTextInput(el: EventTarget | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || el.isContentEditable
}

function handlePTTKeyDown(e: KeyboardEvent): void {
  if (!pttEnabled.value || e.code !== pttKeyCode.value) return
  if (pttKeyHeld) return
  if (isTextInput(e.target)) return
  pttKeyHeld = true
  e.preventDefault()
  PTTKeyDown()
}

function handlePTTKeyUp(e: KeyboardEvent): void {
  if (!pttEnabled.value || e.code !== pttKeyCode.value) return
  if (!pttKeyHeld) return
  pttKeyHeld = false
  PTTKeyUp()
}

function handleGlobalShortcuts(e: KeyboardEvent): void {
  if ((e.ctrlKey && e.key === '/') || (e.key === '?' && !isTextInput(e.target))) {
    e.preventDefault()
    showShortcutsHelp.value = !showShortcutsHelp.value
    return
  }
  if (e.ctrlKey && e.shiftKey && e.code === 'KeyM') {
    e.preventDefault()
    window.dispatchEvent(new CustomEvent('shortcut:mute-toggle'))
    return
  }
  if (isTextInput(e.target)) return
  if ((e.key === 'm' || e.key === 'M') && !e.ctrlKey && !e.altKey && !e.metaKey) {
    window.dispatchEvent(new CustomEvent('shortcut:mute-toggle'))
    return
  }
  if ((e.key === 'd' || e.key === 'D') && !e.ctrlKey && !e.altKey && !e.metaKey) {
    window.dispatchEvent(new CustomEvent('shortcut:deafen-toggle'))
    return
  }
  if (e.key === 'Escape' && showShortcutsHelp.value) {
    showShortcutsHelp.value = false
  }
}

function getLastConnectedAddr(): string {
  try {
    return normaliseAddr(localStorage.getItem(LAST_CONNECTED_ADDR_KEY) ?? '')
  } catch {
    return ''
  }
}

function setLastConnectedAddr(addr: string): void {
  try {
    localStorage.setItem(LAST_CONNECTED_ADDR_KEY, normaliseAddr(addr))
  } catch {
    // Ignore storage failures.
  }
}

async function ensureActiveServerContext(): Promise<boolean> {
  const addr = connectedAddr.value
  if (!addr) {
    return false
  }
  const err = await SetActiveServer(addr)
  if (err) {
    setActiveError(err)
    return false
  }
  return true
}

async function connectToServer(addr: string, username: string): Promise<boolean> {
  const targetAddr = normaliseAddr(addr)
  const user = normaliseUsername(username)
  if (!targetAddr) {
    return false
  }
  if (!user) {
    withServer(targetAddr, state => { state.connectError = 'Set a global username first (right-click your name in User Controls).' })
    return false
  }

  ensureServer(targetAddr)
  activeServerAddr.value = targetAddr

  const err = await Connect(targetAddr, user)
  if (err) {
    withServer(targetAddr, state => {
      state.connectError = err
      state.connected = false
    })
    markConnected(targetAddr, false)
    return false
  }

  markConnected(targetAddr, true)
  withServer(targetAddr, state => { state.connectError = '' })
  setLastConnectedAddr(targetAddr)
  await SetActiveServer(targetAddr)
  startupAddrHint.value = ''
  return true
}

async function handleConnect(payload: ConnectPayload): Promise<void> {
  await connectToServer(payload.addr, payload.username)
}

async function handleSelectServer(addr: string): Promise<void> {
  const targetAddr = normaliseAddr(addr)
  if (!targetAddr) return
  activeServerAddr.value = targetAddr
  ensureServer(targetAddr)
  await SetActiveServer(targetAddr)
}

async function handleActivateChannel(payload: { addr: string; channelID: number }): Promise<void> {
  if (joiningVoice.value) return
  joiningVoice.value = true
  try {
    const ok = await connectToServer(payload.addr, globalUsername.value)
    if (!ok) return
    await SetActiveServer(normaliseAddr(payload.addr))

    if (voiceConnected.value && voiceServerAddr.value === normaliseAddr(payload.addr)) {
      const err = await JoinChannel(payload.channelID)
      if (err) {
        setActiveError(err)
        return
      }
    } else {
      const err = await ConnectVoice(payload.channelID)
      if (err) {
        setActiveError(err)
        return
      }
      voiceConnected.value = true
      voiceServerAddr.value = normaliseAddr(payload.addr)
    }
    setActiveError('')
  } finally {
    joiningVoice.value = false
  }
}

async function handleRenameGlobalUsername(name: string): Promise<void> {
  const next = normaliseUsername(name)
  if (!next) {
    setActiveError('Username cannot be empty.')
    return
  }
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, username: next })
  globalUsername.value = next
  await RenameUser(next)
}

async function handleEditMessage(msgID: number, message: string): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await EditMessage(msgID, message)
}

async function handleDeleteMessage(msgID: number): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await DeleteMessage(msgID)
}

async function handleAddReaction(msgID: number, emoji: string): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await AddReaction(msgID, emoji)
}

async function handleRemoveReaction(msgID: number, emoji: string): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await RemoveReaction(msgID, emoji)
}

async function handleSendChat(message: string): Promise<void> {
  activeChannelId.value = 0
  if (!await ensureActiveServerContext()) return
  await SendChat(message)
}

async function handleSendChannelChat(channelID: number, message: string): Promise<void> {
  activeChannelId.value = channelID
  if (!await ensureActiveServerContext()) return
  await SendChannelChat(channelID, message)
}

async function handleCreateChannel(name: string): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await CreateChannel(name)
}

async function handleRenameChannel(channelID: number, name: string): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await RenameChannel(channelID, name)
}

async function handleDeleteChannel(channelID: number): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await DeleteChannel(channelID)
}

async function handleMoveUser(userID: number, channelID: number): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await MoveUserToChannel(userID, channelID)
}

async function handleKickUser(userID: number): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await KickUser(userID)
}

function handleViewChannel(channelID: number): void {
  const addr = connectedAddr.value
  if (!addr) return
  withServer(addr, state => {
    state.viewedChannelId = channelID
    if (state.unreadCounts[channelID]) {
      const { [channelID]: _, ...rest } = state.unreadCounts
      state.unreadCounts = rest
    }
  })
}

async function handleUploadFile(channelID: number): Promise<void> {
  activeChannelId.value = channelID
  if (!await ensureActiveServerContext()) return
  const err = await UploadFile(channelID)
  if (err) setActiveError(err)
}

async function handleUploadFileFromPath(channelID: number, path: string): Promise<void> {
  activeChannelId.value = channelID
  if (!await ensureActiveServerContext()) return
  const err = await UploadFileFromPath(channelID, path)
  if (err) setActiveError(err)
}

async function handleStartVideo(): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await StartVideo()
}

async function handleStopVideo(): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await StopVideo()
}

async function handleStartScreenShare(): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await StartScreenShare()
}

async function handleStopScreenShare(): Promise<void> {
  if (!await ensureActiveServerContext()) return
  await StopScreenShare()
}

async function handleDisconnectVoice(): Promise<void> {
  if (disconnectingVoice.value || !voiceConnected.value) return
  disconnectingVoice.value = true
  const priorVoiceServer = voiceServerAddr.value
  try {
    const err = await DisconnectVoice()
    if (err) {
      setActiveError(err)
      return
    }
    if (priorVoiceServer) {
      withServer(priorVoiceServer, state => {
        const me = state.myID
        if (!me) return
        state.userChannels = { ...state.userChannels, [me]: 0 }
      })
    }
    voiceConnected.value = false
    voiceServerAddr.value = ''
    clearSpeaking()
  } finally {
    disconnectingVoice.value = false
  }
}

async function handleDisconnect(): Promise<void> {
  const addr = connectedAddr.value
  if (!addr) return
  const err = await DisconnectServer(addr)
  if (err) {
    setActiveError(err)
    return
  }
  markConnected(addr, false)
  if (voiceServerAddr.value === addr) {
    voiceConnected.value = false
    voiceServerAddr.value = ''
    clearSpeaking()
  }
  withServer(addr, state => {
    state.connected = false
  })
  const fallback = connectedServers.value[0] ?? Object.keys(serverStates.value)[0] ?? ''
  activeServerAddr.value = fallback
  if (fallback) await SetActiveServer(fallback)
}

async function handleCancelReconnect(): Promise<void> {
  reconnecting.value = false
}

onMounted(async () => {
  syncRouteFromHash()
  window.addEventListener('hashchange', syncRouteFromHash)

  EventsOn('server:connected', (data: { server_addr: string }) => {
    const addr = normaliseAddr(data?.server_addr || '')
    if (!addr) return
    markConnected(addr, true)
  })

  EventsOn('server:disconnected', (data: { server_addr: string; reason?: string }) => {
    const addr = normaliseAddr(data?.server_addr || '')
    if (!addr) return
    markConnected(addr, false)
    if (data?.reason) {
      withServer(addr, state => { state.connectError = data.reason || '' })
    }
    if (voiceServerAddr.value === addr) {
      voiceConnected.value = false
      voiceServerAddr.value = ''
      clearSpeaking()
    }
  })

  EventsOn('connection:lost', (data: { server_addr: string; reason: string } | null) => {
    const addr = normaliseAddr(data?.server_addr || '')
    if (!addr) return
    markConnected(addr, false)
    withServer(addr, state => { state.connectError = data?.reason || 'Connection lost' })
    if (voiceServerAddr.value === addr) {
      voiceConnected.value = false
      voiceServerAddr.value = ''
      clearSpeaking()
    }
  })

  EventsOn('user:list', (data: any) => {
    const addr = eventServerAddr(data)
    const list = Array.isArray(data) ? data as User[] : (data?.users ?? []) as User[]
    withServer(addr, state => {
      state.users = list
      const map: Record<number, number> = {}
      for (const u of list) map[u.id] = u.channel_id ?? 0
      state.userChannels = map
      state.connected = true
      if (state.channels.length > 0 && state.viewedChannelId === 0) {
        state.viewedChannelId = state.channels[0].id
      }
    })
  })

  EventsOn('user:joined', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      state.users = [...state.users, { id: data.id, username: data.username }]
      state.userChannels = { ...state.userChannels, [data.id]: 0 }
      const first = state.channels.length > 0 ? state.channels[0].id : 0
      state.chatMessages = [...state.chatMessages, {
        id: ++chatIdCounter, msgId: 0, senderId: 0, username: '', message: `${data.username} joined the server`,
        ts: Date.now(), channelId: first, system: true,
      }]
    })
  })

  EventsOn('user:left', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const leftUser = state.users.find(u => u.id === data.id)
      state.users = state.users.filter(u => u.id !== data.id)
      const { [data.id]: _, ...rest } = state.userChannels
      state.userChannels = rest
      if (leftUser) {
        const first = state.channels.length > 0 ? state.channels[0].id : 0
        state.chatMessages = [...state.chatMessages, {
          id: ++chatIdCounter, msgId: 0, senderId: 0, username: '', message: `${leftUser.username} left the server`,
          ts: Date.now(), channelId: first, system: true,
        }]
      }
      if (state.videoStates[data.id]) {
        const { [data.id]: __, ...vs } = state.videoStates
        state.videoStates = vs
      }
    })
  })

  EventsOn('user:renamed', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      state.users = state.users.map(u => u.id === data.id ? { ...u, username: data.username } : u)
    })
  })

  EventsOn('channel:list', (data: any) => {
    const addr = eventServerAddr(data)
    const list = Array.isArray(data) ? data as Channel[] : (data?.channels ?? []) as Channel[]
    withServer(addr, state => {
      state.channels = list
      if (!list.some(ch => ch.id === state.viewedChannelId)) {
        state.viewedChannelId = list.length > 0 ? list[0].id : 0
      }
    })
  })

  EventsOn('channel:user_moved', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      state.userChannels = { ...state.userChannels, [data.user_id]: data.channel_id }
    })
  })

  EventsOn('chat:message', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const fallback = state.channels.length > 0 ? state.channels[0].id : 0
      const channelId = data.channel_id ?? fallback
      state.chatMessages = [...state.chatMessages, {
        id: ++chatIdCounter,
        msgId: data.msg_id ?? 0,
        senderId: data.sender_id ?? 0,
        username: data.username,
        message: data.message,
        ts: data.ts,
        channelId,
        fileId: data.file_id,
        fileName: data.file_name,
        fileSize: data.file_size,
        fileUrl: data.file_url,
        mentions: data.mentions,
        replyTo: data.reply_to,
        replyPreview: data.reply_preview,
      }]
      if (data.sender_id && state.typingUsers[data.sender_id]) {
        const { [data.sender_id]: _, ...rest } = state.typingUsers
        state.typingUsers = rest
      }
      if (channelId !== state.viewedChannelId) {
        state.unreadCounts = { ...state.unreadCounts, [channelId]: (state.unreadCounts[channelId] ?? 0) + 1 }
      }
    })
  })

  EventsOn('chat:link_preview', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
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
      state.chatMessages = updated
    })
  })

  EventsOn('chat:message_edited', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], message: data.message, edited: true, editedTs: data.ts }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:message_deleted', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], message: '', deleted: true }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:reaction_added', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      const msg = { ...updated[idx] }
      const reactions = [...(msg.reactions ?? [])]
      const rxIdx = reactions.findIndex(r => r.emoji === data.emoji)
      if (rxIdx >= 0) {
        const rx = { ...reactions[rxIdx] }
        if (!rx.user_ids.includes(data.id)) {
          rx.user_ids = [...rx.user_ids, data.id]
          rx.count = rx.user_ids.length
        }
        reactions[rxIdx] = rx
      } else {
        reactions.push({ emoji: data.emoji, user_ids: [data.id], count: 1 } as ReactionInfo)
      }
      msg.reactions = reactions
      updated[idx] = msg
      state.chatMessages = updated
    })
  })

  EventsOn('chat:reaction_removed', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      const msg = { ...updated[idx] }
      let reactions = [...(msg.reactions ?? [])]
      const rxIdx = reactions.findIndex(r => r.emoji === data.emoji)
      if (rxIdx >= 0) {
        const rx = { ...reactions[rxIdx] }
        rx.user_ids = rx.user_ids.filter((id: number) => id !== data.id)
        rx.count = rx.user_ids.length
        if (rx.count === 0) {
          reactions = reactions.filter((_, i) => i !== rxIdx)
        } else {
          reactions[rxIdx] = rx
        }
      }
      msg.reactions = reactions
      updated[idx] = msg
      state.chatMessages = updated
    })
  })

  EventsOn('chat:user_typing', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      if (data.id === state.myID) return
      state.typingUsers = {
        ...state.typingUsers,
        [data.id]: { username: data.username, channelId: data.channel_id, expiresAt: Date.now() + 5000 },
      }
    })
  })

  EventsOn('chat:message_pinned', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], pinned: true }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:message_unpinned', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], pinned: false }
      state.chatMessages = updated
    })
  })

  EventsOn('server:info', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => { state.serverName = data.name })
  })

  EventsOn('room:owner', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => { state.ownerID = data.owner_id })
  })

  EventsOn('user:me', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => { state.myID = data.id })
  })

  EventsOn('audio:speaking', (data: any) => {
    if (data?.id !== undefined) setSpeaking(data.id)
  })

  EventsOn('video:state', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      if (data.video_active) {
        state.videoStates = { ...state.videoStates, [data.id]: { active: true, screenShare: data.screen_share } }
      } else {
        const { [data.id]: _, ...rest } = state.videoStates
        state.videoStates = rest
      }
    })
  })

  EventsOn('video:layers', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      const existing = state.videoStates[data.id]
      if (!existing) return
      state.videoStates = { ...state.videoStates, [data.id]: { ...existing, layers: data.layers } }
    })
  })

  EventsOn('recording:state', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      if (data.recording) {
        state.recordingChannels = { ...state.recordingChannels, [data.channel_id]: { recording: true, startedBy: data.started_by } }
      } else {
        const { [data.channel_id]: _, ...rest } = state.recordingChannels
        state.recordingChannels = rest
      }
    })
  })

  EventsOn('connection:kicked', (data: any) => {
    const addr = eventServerAddr(data)
    withServer(addr, state => {
      state.connectError = 'Disconnected by server owner'
      state.connected = false
    })
    markConnected(addr, false)
    if (voiceServerAddr.value === addr) {
      voiceConnected.value = false
      voiceServerAddr.value = ''
      clearSpeaking()
    }
  })

  EventsOn('file:dropped', async (data: { paths: string[] }) => {
    if (!connected.value || !data.paths?.length) return
    if (!await ensureActiveServerContext()) return
    for (const path of data.paths) {
      const err = await UploadFileFromPath(activeChannelId.value, path)
      if (err) {
        setActiveError(err)
        break
      }
    }
  })

  typingCleanupInterval = setInterval(() => {
    const now = Date.now()
    const next: Record<string, ServerState> = { ...serverStates.value }
    for (const [addr, state] of Object.entries(next)) {
      const typing: typeof state.typingUsers = {}
      for (const [id, entry] of Object.entries(state.typingUsers)) {
        if (entry.expiresAt > now) typing[Number(id)] = entry
      }
      next[addr] = { ...state, typingUsers: typing }
    }
    serverStates.value = next
  }, 1000)

  window.addEventListener('keydown', handleGlobalShortcuts)
  window.addEventListener('keydown', handlePTTKeyDown)
  window.addEventListener('keyup', handlePTTKeyUp)
  window.addEventListener('ptt-config-changed', ((e: CustomEvent) => {
    pttEnabled.value = e.detail.enabled
    pttKeyCode.value = e.detail.key
    pttKeyHeld = false
  }) as EventListener)

  await ApplyConfig()

  window.addEventListener('density-changed', ((e: CustomEvent) => {
    messageDensity.value = e.detail
  }) as EventListener)
  window.addEventListener('system-messages-changed', ((e: CustomEvent) => {
    showSystemMessages.value = e.detail
  }) as EventListener)

  const cfg = await GetConfig()
  globalUsername.value = cfg.username?.trim() ?? ''
  pttEnabled.value = cfg.ptt_enabled ?? false
  pttKeyCode.value = cfg.ptt_key || 'Backquote'
  messageDensity.value = cfg.message_density ?? 'default'
  showSystemMessages.value = cfg.show_system_messages ?? true

  if (!globalUsername.value) {
    const hex = Array.from(crypto.getRandomValues(new Uint8Array(2)))
      .map(b => b.toString(16).padStart(2, '0'))
      .join('')
    const generated = `User-${hex}`
    await SaveConfig({ ...cfg, username: generated })
    globalUsername.value = generated
  }

  const [auto, startupAddr] = await Promise.all([GetAutoLogin(), GetStartupAddr()])
  if (auto.username) globalUsername.value = auto.username

  if (auto.username) {
    await connectToServer(auto.addr, auto.username)
  } else if (startupAddr && globalUsername.value) {
    await connectToServer(startupAddr, globalUsername.value)
  } else if (globalUsername.value) {
    const lastAddr = getLastConnectedAddr()
    if (lastAddr) {
      const ok = await connectToServer(lastAddr, globalUsername.value)
      if (ok) return
    }
    if (cfg.servers?.length) {
      await connectToServer(cfg.servers[0].addr, globalUsername.value)
    }
  } else if (startupAddr) {
    startupAddrHint.value = startupAddr
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncRouteFromHash)
  window.removeEventListener('keydown', handleGlobalShortcuts)
  window.removeEventListener('keydown', handlePTTKeyDown)
  window.removeEventListener('keyup', handlePTTKeyUp)
  EventsOff('connection:lost', 'server:connected', 'server:disconnected', 'user:list', 'user:joined', 'user:left', 'user:renamed', 'chat:message', 'chat:message_edited', 'chat:message_deleted', 'chat:link_preview', 'chat:reaction_added', 'chat:reaction_removed', 'chat:user_typing', 'chat:message_pinned', 'chat:message_unpinned', 'server:info', 'room:owner', 'user:me', 'connection:kicked', 'channel:list', 'channel:user_moved', 'audio:speaking', 'video:state', 'video:layers', 'recording:state', 'file:dropped')
  cleanupSpeaking()
  if (typingCleanupInterval) clearInterval(typingCleanupInterval)
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
          :connected-addrs="connectedServers"
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
          :video-states="videoStates"
          :recording-channels="recordingChannels"
          :typing-users="typingUsers"
          :message-density="messageDensity"
          :show-system-messages="showSystemMessages"
          @connect="handleConnect"
          @select-server="handleSelectServer"
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
          @kick-user="handleKickUser"
          @upload-file="handleUploadFile"
          @upload-file-from-path="handleUploadFileFromPath"
          @view-channel="handleViewChannel"
          @edit-message="handleEditMessage"
          @delete-message="handleDeleteMessage"
          @add-reaction="handleAddReaction"
          @remove-reaction="handleRemoveReaction"
          @start-video="handleStartVideo"
          @stop-video="handleStopVideo"
          @start-screen-share="handleStartScreenShare"
          @stop-screen-share="handleStopScreenShare"
        />
      </Transition>
    </div>
    <KeyboardShortcuts v-if="showShortcutsHelp" @close="showShortcutsHelp = false" />
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
