<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, DisconnectVoice, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat, SendChannelChat, GetStartupAddr, GetConfig, SaveConfig, JoinChannel, ConnectVoice, CreateChannel, RenameChannel, DeleteChannel, MoveUserToChannel, KickUser, UploadFile, UploadFileFromPath, PTTKeyDown, PTTKeyUp, RenameUser, EditMessage, DeleteMessage, AddReaction, RemoveReaction, SendTyping, StartVideo, StopVideo, StartScreenShare, StopScreenShare } from './config'
import type { ServerEntry } from './config'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import ChannelView from './ChannelView.vue'
import SettingsPage from './SettingsPage.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import TitleBar from './TitleBar.vue'
import KeyboardShortcuts from './KeyboardShortcuts.vue'
import { useSpeakingUsers } from './composables/useSpeakingUsers'
import { BKEN_SCHEME, LAST_CONNECTED_ADDR_KEY } from './constants'
import type { User, ConnectPayload, ChatMessage, Channel, VideoState, ReactionInfo } from './types'

type AppRoute = 'channel' | 'settings'

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
const currentRoute = ref<AppRoute>('channel')
const globalUsername = ref('')
const joiningVoice = ref(false)
const disconnectingVoice = ref(false)
const showShortcutsHelp = ref(false)
const messageDensity = ref<'compact' | 'default' | 'comfortable'>('default')
const showSystemMessages = ref(true)
const voiceConnected = ref(false)
const activeChannelId = ref(0)

const serverAddr = ref('')
const serverState = ref<ServerState>(emptyServerState())
const savedServers = ref<ServerEntry[]>([{ name: 'Local Dev', addr: 'localhost:8080' }])
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

const connected = computed(() => serverState.value.connected)
const connectedAddr = computed(() => serverAddr.value)
const users = computed(() => serverState.value.users)
const chatMessages = computed(() => serverState.value.chatMessages)
const serverName = computed(() => serverState.value.serverName)
const ownerID = computed(() => serverState.value.ownerID)
const myID = computed(() => serverState.value.myID)
const channels = computed(() => serverState.value.channels)
const userChannels = computed(() => serverState.value.userChannels)
const unreadCounts = computed(() => serverState.value.unreadCounts)
const videoStates = computed(() => serverState.value.videoStates)
const recordingChannels = computed(() => serverState.value.recordingChannels)
const typingUsers = computed(() => serverState.value.typingUsers)
const connectError = computed(() => serverState.value.connectError)

function setActiveError(message: string): void {
  serverState.value = { ...serverState.value, connectError: message }
}

function parseRoute(hash: string): AppRoute {
  return hash === '#/settings' ? 'settings' : 'channel'
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
  goToRoute('channel')
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

/** Helper to update serverState fields. */
function updateState(updater: (state: ServerState) => void): void {
  const s = { ...serverState.value }
  updater(s)
  serverState.value = s
}

async function connectToServer(addr: string, username: string): Promise<boolean> {
  const targetAddr = normaliseAddr(addr)
  const user = normaliseUsername(username)
  if (!targetAddr) {
    return false
  }
  if (!user) {
    setActiveError('Set a global username first (right-click your name in User Controls).')
    return false
  }

  const err = await Connect(targetAddr, user)
  if (err) {
    serverState.value = { ...serverState.value, connectError: err, connected: false }
    return false
  }

  serverAddr.value = targetAddr
  serverState.value = { ...serverState.value, connected: true, connectError: '' }
  setLastConnectedAddr(targetAddr)
  startupAddrHint.value = ''
  return true
}

async function handleConnect(payload: ConnectPayload): Promise<void> {
  await connectToServer(payload.addr, payload.username)
}

async function handleSelectServer(addr: string): Promise<void> {
  const targetAddr = normaliseAddr(addr)
  if (!targetAddr) return
  if (targetAddr === serverAddr.value && connected.value) return
  await connectToServer(targetAddr, globalUsername.value)
}

async function handleActivateChannel(payload: { addr: string; channelID: number }): Promise<void> {
  if (joiningVoice.value) return
  joiningVoice.value = true
  try {
    const targetAddr = normaliseAddr(payload.addr)
    // If not connected or connecting to a different server, connect first
    if (!connected.value || targetAddr !== serverAddr.value) {
      const ok = await connectToServer(targetAddr, globalUsername.value)
      if (!ok) return
    }

    if (voiceConnected.value) {
      // Already in voice — just switch channel
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
  if (!connected.value) return
  await EditMessage(msgID, message)
}

async function handleDeleteMessage(msgID: number): Promise<void> {
  if (!connected.value) return
  await DeleteMessage(msgID)
}

async function handleAddReaction(msgID: number, emoji: string): Promise<void> {
  if (!connected.value) return
  await AddReaction(msgID, emoji)
}

async function handleRemoveReaction(msgID: number, emoji: string): Promise<void> {
  if (!connected.value) return
  await RemoveReaction(msgID, emoji)
}

async function handleSendChat(message: string): Promise<void> {
  activeChannelId.value = 0
  if (!connected.value) return
  await SendChat(message)
}

async function handleSendChannelChat(channelID: number, message: string): Promise<void> {
  activeChannelId.value = channelID
  if (!connected.value) return
  await SendChannelChat(channelID, message)
}

async function handleCreateChannel(name: string): Promise<void> {
  if (!connected.value) return
  await CreateChannel(name)
}

async function handleRenameChannel(channelID: number, name: string): Promise<void> {
  if (!connected.value) return
  await RenameChannel(channelID, name)
}

async function handleDeleteChannel(channelID: number): Promise<void> {
  if (!connected.value) return
  await DeleteChannel(channelID)
}

async function handleMoveUser(userID: number, channelID: number): Promise<void> {
  if (!connected.value) return
  await MoveUserToChannel(userID, channelID)
}

async function handleKickUser(userID: number): Promise<void> {
  if (!connected.value) return
  await KickUser(userID)
}

function handleViewChannel(channelID: number): void {
  updateState(state => {
    state.viewedChannelId = channelID
    if (state.unreadCounts[channelID]) {
      const { [channelID]: _, ...rest } = state.unreadCounts
      state.unreadCounts = rest
    }
  })
}

async function handleUploadFile(channelID: number): Promise<void> {
  activeChannelId.value = channelID
  if (!connected.value) return
  const err = await UploadFile(channelID)
  if (err) setActiveError(err)
}

async function handleUploadFileFromPath(channelID: number, path: string): Promise<void> {
  activeChannelId.value = channelID
  if (!connected.value) return
  const err = await UploadFileFromPath(channelID, path)
  if (err) setActiveError(err)
}

async function handleStartVideo(): Promise<void> {
  if (!connected.value) return
  await StartVideo()
}

async function handleStopVideo(): Promise<void> {
  if (!connected.value) return
  await StopVideo()
}

async function handleStartScreenShare(): Promise<void> {
  if (!connected.value) return
  await StartScreenShare()
}

async function handleStopScreenShare(): Promise<void> {
  if (!connected.value) return
  await StopScreenShare()
}

async function handleDisconnectVoice(): Promise<void> {
  if (disconnectingVoice.value || !voiceConnected.value) return
  disconnectingVoice.value = true
  try {
    const err = await DisconnectVoice()
    if (err) {
      setActiveError(err)
    }
  } finally {
    // Always clean up local voice state, even if the server call failed.
    // The Go layer has already stopped audio capture; the user is no longer
    // transmitting. Keeping the avatar in the channel would be misleading.
    updateState(state => {
      const me = state.myID
      if (me) {
        state.userChannels = { ...state.userChannels, [me]: 0 }
      }
    })
    voiceConnected.value = false
    clearSpeaking()
    disconnectingVoice.value = false
  }
}

async function handleDisconnect(): Promise<void> {
  if (!serverAddr.value) return
  await Disconnect()
  voiceConnected.value = false
  clearSpeaking()
  serverAddr.value = ''
  serverState.value = emptyServerState()
}

async function handleCancelReconnect(): Promise<void> {
  reconnecting.value = false
}

onMounted(async () => {
  syncRouteFromHash()
  window.addEventListener('hashchange', syncRouteFromHash)

  EventsOn('server:connected', (_data: { server_addr: string }) => {
    serverState.value = { ...serverState.value, connected: true }
  })

  EventsOn('server:disconnected', (data: { server_addr: string; reason?: string }) => {
    serverState.value = { ...serverState.value, connected: false, connectError: data?.reason || '' }
    voiceConnected.value = false
    clearSpeaking()
  })

  EventsOn('connection:lost', (data: { server_addr: string; reason: string } | null) => {
    serverState.value = { ...serverState.value, connected: false, connectError: data?.reason || 'Connection lost' }
    voiceConnected.value = false
    clearSpeaking()
  })

  EventsOn('user:list', (data: any) => {
    const list = Array.isArray(data) ? data as User[] : (data?.users ?? []) as User[]
    updateState(state => {
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
    updateState(state => {
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
    updateState(state => {
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
    updateState(state => {
      state.users = state.users.map(u => u.id === data.id ? { ...u, username: data.username } : u)
    })
  })

  EventsOn('channel:list', (data: any) => {
    const list = Array.isArray(data) ? data as Channel[] : (data?.channels ?? []) as Channel[]
    updateState(state => {
      state.channels = list
      if (!list.some(ch => ch.id === state.viewedChannelId)) {
        state.viewedChannelId = list.length > 0 ? list[0].id : 0
      }
    })
  })

  EventsOn('channel:user_moved', (data: any) => {
    updateState(state => {
      state.userChannels = { ...state.userChannels, [data.user_id]: data.channel_id }
    })
  })

  EventsOn('chat:message', (data: any) => {
    updateState(state => {
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
    updateState(state => {
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
    updateState(state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], message: data.message, edited: true, editedTs: data.ts }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:message_deleted', (data: any) => {
    updateState(state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], message: '', deleted: true }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:reaction_added', (data: any) => {
    updateState(state => {
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
    updateState(state => {
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
    updateState(state => {
      if (data.id === state.myID) return
      state.typingUsers = {
        ...state.typingUsers,
        [data.id]: { username: data.username, channelId: data.channel_id, expiresAt: Date.now() + 5000 },
      }
    })
  })

  EventsOn('chat:message_pinned', (data: any) => {
    updateState(state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], pinned: true }
      state.chatMessages = updated
    })
  })

  EventsOn('chat:message_unpinned', (data: any) => {
    updateState(state => {
      const idx = state.chatMessages.findIndex(m => m.msgId === data.msg_id)
      if (idx === -1) return
      const updated = [...state.chatMessages]
      updated[idx] = { ...updated[idx], pinned: false }
      state.chatMessages = updated
    })
  })

  EventsOn('server:info', (data: any) => {
    updateState(state => { state.serverName = data.name })
  })

  EventsOn('channel:owner', (data: any) => {
    updateState(state => { state.ownerID = data.owner_id })
  })

  EventsOn('user:me', (data: any) => {
    updateState(state => { state.myID = data.id })
  })

  EventsOn('audio:speaking', (data: any) => {
    if (data?.id !== undefined) setSpeaking(data.id)
  })

  EventsOn('video:state', (data: any) => {
    updateState(state => {
      if (data.video_active) {
        state.videoStates = { ...state.videoStates, [data.id]: { active: true, screenShare: data.screen_share } }
      } else {
        const { [data.id]: _, ...rest } = state.videoStates
        state.videoStates = rest
      }
    })
  })

  EventsOn('video:layers', (data: any) => {
    updateState(state => {
      const existing = state.videoStates[data.id]
      if (!existing) return
      state.videoStates = { ...state.videoStates, [data.id]: { ...existing, layers: data.layers } }
    })
  })

  EventsOn('recording:state', (data: any) => {
    updateState(state => {
      if (data.recording) {
        state.recordingChannels = { ...state.recordingChannels, [data.channel_id]: { recording: true, startedBy: data.started_by } }
      } else {
        const { [data.channel_id]: _, ...rest } = state.recordingChannels
        state.recordingChannels = rest
      }
    })
  })

  EventsOn('connection:kicked', (_data: any) => {
    serverState.value = { ...serverState.value, connectError: 'Disconnected by server owner', connected: false }
    voiceConnected.value = false
    clearSpeaking()
  })

  EventsOn('file:dropped', async (data: { paths: string[] }) => {
    if (!connected.value || !data.paths?.length) return
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
    const s = serverState.value
    const typing: typeof s.typingUsers = {}
    for (const [id, entry] of Object.entries(s.typingUsers)) {
      if (entry.expiresAt > now) typing[Number(id)] = entry
    }
    serverState.value = { ...s, typingUsers: typing }
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
  if (cfg.servers?.length) {
    savedServers.value = cfg.servers
  }

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

  // Only auto-connect for explicit auto-login (CLI arg / protocol handler).
  // Otherwise show WelcomePage and let the user choose.
  if (auto.username && auto.addr) {
    await connectToServer(auto.addr, auto.username)
  } else if (startupAddr) {
    startupAddrHint.value = startupAddr
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncRouteFromHash)
  window.removeEventListener('keydown', handleGlobalShortcuts)
  window.removeEventListener('keydown', handlePTTKeyDown)
  window.removeEventListener('keyup', handlePTTKeyUp)
  EventsOff('connection:lost', 'server:connected', 'server:disconnected', 'user:list', 'user:joined', 'user:left', 'user:renamed', 'chat:message', 'chat:message_edited', 'chat:message_deleted', 'chat:link_preview', 'chat:reaction_added', 'chat:reaction_removed', 'chat:user_typing', 'chat:message_pinned', 'chat:message_unpinned', 'server:info', 'channel:owner', 'user:me', 'connection:kicked', 'channel:list', 'channel:user_moved', 'audio:speaking', 'video:state', 'video:layers', 'recording:state', 'file:dropped')
  cleanupSpeaking()
  if (typingCleanupInterval) clearInterval(typingCleanupInterval)
})
</script>

<template>
  <main class="grid grid-rows-[auto_auto_minmax(0,1fr)] h-full">
    <TitleBar :server-name="serverName" :is-owner="ownerID !== 0 && ownerID === myID" :server-addr="connectedAddr" :voice-connected="voiceConnected" />

    <div>
      <Transition
        enter-active-class="transition-all duration-200 ease-out"
        enter-from-class="-translate-y-full opacity-0"
        leave-active-class="transition-all duration-200 ease-out overflow-hidden"
        leave-to-class="-translate-y-full opacity-0"
      >
        <ReconnectBanner
          v-if="reconnecting"
          :attempt="reconnectAttempt"
          :seconds-until-retry="reconnectSecondsLeft"
          :reason="connectError"
          @cancel="handleCancelReconnect"
        />
      </Transition>
    </div>

    <div class="min-h-0">
      <Transition
        mode="out-in"
        enter-active-class="transition-opacity duration-150 ease-in-out"
        enter-from-class="opacity-0"
        leave-active-class="transition-opacity duration-150 ease-in-out"
        leave-to-class="opacity-0"
      >
        <SettingsPage
          v-if="currentRoute === 'settings'"
          key="settings"
          class="h-full min-h-0"
          @back="closeSettingsPage"
        />

        <ChannelView
          v-else
          key="channel"
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
          :video-states="videoStates"
          :recording-channels="recordingChannels"
          :typing-users="typingUsers"
          :message-density="messageDensity"
          :show-system-messages="showSystemMessages"
          :servers="savedServers"
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

