<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { Connect, Disconnect, DisconnectVoice, GetAutoLogin } from '../wailsjs/go/main/App'
import { ApplyConfig, SendChat, SendChannelChat, GetStartupAddr, GetConfig, SaveConfig, JoinChannel, ConnectVoice, CreateChannel, RenameChannel, DeleteChannel, MoveUserToChannel, KickUser, UploadFile, UploadFileFromPath, PTTKeyDown, PTTKeyUp, RenameUser, EditMessage, DeleteMessage, AddReaction, RemoveReaction, SendTyping, StartVideo, StopVideo, StartScreenShare, StopScreenShare } from './config'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import Room from './Room.vue'
import SettingsPage from './SettingsPage.vue'
import ReconnectBanner from './ReconnectBanner.vue'
import TitleBar from './TitleBar.vue'
import KeyboardShortcuts from './KeyboardShortcuts.vue'
import { useReconnect } from './composables/useReconnect'
import { useSpeakingUsers } from './composables/useSpeakingUsers'
import { BKEN_SCHEME, LAST_CONNECTED_ADDR_KEY } from './constants'
import type { User, UserJoinedEvent, UserLeftEvent, ConnectPayload, ChatMessage, Channel, SpeakingEvent, VideoState, ReactionInfo } from './types'

type AppRoute = 'room' | 'settings'

const connected = ref(false)
const voiceConnected = ref(false)
const users = ref<User[]>([])
const chatMessages = ref<ChatMessage[]>([])
const serverName = ref('')
const ownerID = ref(0)
const myID = ref(0)
const channels = ref<Channel[]>([])
/** Maps userID -> channelID. Updated by user:list, user:joined, user:left, channel:user_moved. */
const userChannels = ref<Record<number, number>>({})
const firstChannelId = computed(() => channels.value.length > 0 ? channels.value[0].id : 0)

let chatIdCounter = 0

const activeChannelId = ref(0) // tracks the currently-viewed chatroom channel (for file drops)
const viewedChannelId = ref(0) // tracks which channel's chat the user is currently viewing
const unreadCounts = ref<Record<number, number>>({}) // channelId -> unread message count
const videoStates = ref<Record<number, VideoState>>({}) // userId -> video state
const recordingChannels = ref<Record<number, { recording: boolean; startedBy: string }>>({}) // channelId -> recording state
const connectError = ref('')
const startupAddrHint = ref('')
const currentRoute = ref<AppRoute>('room')
const globalUsername = ref('')
const joiningVoice = ref(false)
const disconnectingVoice = ref(false)
const showShortcutsHelp = ref(false)
const messageDensity = ref<'compact' | 'default' | 'comfortable'>('default')
const typingUsers = ref<Record<number, { username: string; channelId: number; expiresAt: number }>>({})
let typingCleanupInterval: ReturnType<typeof setInterval> | null = null
const showSystemMessages = ref(true)

// Push-to-Talk state
const pttEnabled = ref(false)
const pttKeyCode = ref('Backquote')
let pttKeyHeld = false // raw flag to ignore key-repeat events

function isTextInput(el: EventTarget | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || el.isContentEditable
}

function handlePTTKeyDown(e: KeyboardEvent): void {
  if (!pttEnabled.value || e.code !== pttKeyCode.value) return
  if (pttKeyHeld) return // ignore key-repeat
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
  // Ctrl+/ or ? => shortcuts help
  if ((e.ctrlKey && e.key === '/') || (e.key === '?' && !isTextInput(e.target))) {
    e.preventDefault()
    showShortcutsHelp.value = !showShortcutsHelp.value
    return
  }
  // Ctrl+Shift+M => toggle mute (works even in text input)
  if (e.ctrlKey && e.shiftKey && e.code === 'KeyM') {
    e.preventDefault()
    window.dispatchEvent(new CustomEvent('shortcut:mute-toggle'))
    return
  }
  // Skip remaining shortcuts if typing in a text input
  if (isTextInput(e.target)) return
  // M => toggle mute
  if (e.key === 'm' || e.key === 'M') {
    if (!e.ctrlKey && !e.altKey && !e.metaKey) {
      window.dispatchEvent(new CustomEvent('shortcut:mute-toggle'))
      return
    }
  }
  // D => toggle deafen
  if (e.key === 'd' || e.key === 'D') {
    if (!e.ctrlKey && !e.altKey && !e.metaKey) {
      window.dispatchEvent(new CustomEvent('shortcut:deafen-toggle'))
      return
    }
  }
  // Escape => close modals/shortcuts
  if (e.key === 'Escape') {
    if (showShortcutsHelp.value) {
      showShortcutsHelp.value = false
      return
    }
  }
}

const { reconnecting, reconnectAttempt, reconnectSecondsLeft, startReconnect, cancelReconnect, clearTimers, setLastCredentials } = useReconnect()
const { speakingUsers, setSpeaking, clearSpeaking, cleanup: cleanupSpeaking } = useSpeakingUsers()

/** The server address the client is currently connected to. Exposed to TitleBar so the owner can generate an invite link. */
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
  videoStates.value = {}
  recordingChannels.value = {}
  typingUsers.value = {}
}

function normaliseUsername(name: string): string {
  return name.trim()
}

function normaliseAddr(addr: string): string {
  const cleaned = addr.trim()
  return cleaned.startsWith(BKEN_SCHEME) ? cleaned.slice(BKEN_SCHEME.length) : cleaned
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
    // Ignore storage failures; reconnect fallback is best-effort.
  }
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

  if (connected.value && connectedAddr.value === targetAddr && channels.value.length > 0) {
    return true
  }

  if (connected.value && connectedAddr.value !== targetAddr) {
    await Disconnect()
    resetState()
  }

  setLastCredentials(targetAddr, user)
  connectError.value = ''

  let err = await Connect(targetAddr, user)

  // In dev/hot-reload flows, the backend can still hold an old session while
  // the frontend state is fresh. Force a clean reconnect so we receive
  // user_list/channel_list snapshots again.
  if (err === 'already connected') {
    await Disconnect()
    resetState()
    err = await Connect(targetAddr, user)
  }

  if (err) {
    connectError.value = err
    return false
  }

  connected.value = true
  // Connecting to a server should not auto-join voice.
  voiceConnected.value = false
  connectedAddr.value = targetAddr
  setLastConnectedAddr(targetAddr)
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

  // Notify the server so future chat messages use the new name.
  if (connected.value) {
    await RenameUser(next)
  }

  if (connectError.value.includes('username')) {
    connectError.value = ''
  }
}

async function handleEditMessage(msgID: number, message: string): Promise<void> {
  await EditMessage(msgID, message)
}

async function handleDeleteMessage(msgID: number): Promise<void> {
  await DeleteMessage(msgID)
}

async function handleAddReaction(msgID: number, emoji: string): Promise<void> {
  await AddReaction(msgID, emoji)
}

async function handleRemoveReaction(msgID: number, emoji: string): Promise<void> {
  await RemoveReaction(msgID, emoji)
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

async function handleKickUser(userID: number): Promise<void> {
  await KickUser(userID)
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

async function handleStartVideo(): Promise<void> {
  await StartVideo()
}

async function handleStopVideo(): Promise<void> {
  await StopVideo()
}

async function handleStartScreenShare(): Promise<void> {
  await StartScreenShare()
}

async function handleStopScreenShare(): Promise<void> {
  await StopScreenShare()
}

async function handleDisconnectVoice(): Promise<void> {
  if (disconnectingVoice.value || !voiceConnected.value) return
  disconnectingVoice.value = true

  // Optimistically reflect voice disconnect in the UI so one click is enough.
  voiceConnected.value = false
  clearSpeaking()

  try {
    const err = await DisconnectVoice()
    if (err) {
      connectError.value = err
    }
  } finally {
    disconnectingVoice.value = false
  }
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
    const hadVoice = voiceConnected.value
    // Remember the last voice channel so we can rejoin only if voice was active.
    const lastChannel = userChannels.value[myID.value] ?? 0
    connected.value = false
    voiceConnected.value = false
    connectError.value = reason
    startReconnect(
      async () => {
        connected.value = true
        // Restore voice only if it was previously active.
        if (hadVoice && lastChannel > 0) {
          const err = await ConnectVoice(lastChannel)
          if (err) {
            voiceConnected.value = false
            connectError.value = err
            return
          }
          voiceConnected.value = true
        } else {
          voiceConnected.value = false
        }
        connectError.value = ''
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
    // System message
    chatMessages.value = [...chatMessages.value, {
      id: ++chatIdCounter, msgId: 0, senderId: 0, username: '', message: `${data.username} joined the server`,
      ts: Date.now(), channelId: firstChannelId.value, system: true,
    }]
  })

  EventsOn('user:left', (data: UserLeftEvent) => {
    const leftUser = users.value.find(u => u.id === data.id)
    users.value = users.value.filter(u => u.id !== data.id)
    const { [data.id]: _, ...rest } = userChannels.value
    userChannels.value = rest
    // System message
    if (leftUser) {
      chatMessages.value = [...chatMessages.value, {
        id: ++chatIdCounter, msgId: 0, senderId: 0, username: '', message: `${leftUser.username} left the server`,
        ts: Date.now(), channelId: firstChannelId.value, system: true,
      }]
    }
    // Clean up video state for departed user.
    if (videoStates.value[data.id]) {
      const { [data.id]: __, ...vs } = videoStates.value
      videoStates.value = vs
    }
  })

  EventsOn('user:renamed', (data: { id: number; username: string }) => {
    users.value = users.value.map(u =>
      u.id === data.id ? { ...u, username: data.username } : u
    )
  })

  EventsOn('channel:list', (data: Channel[]) => {
    channels.value = data || []
  })

  EventsOn('channel:user_moved', (data: { user_id: number; channel_id: number }) => {
    userChannels.value = { ...userChannels.value, [data.user_id]: data.channel_id }
  })

  EventsOn('chat:message', (data: { username: string; message: string; ts: number; channel_id: number; msg_id: number; sender_id?: number; file_id?: number; file_name?: string; file_size?: number; file_url?: string; mentions?: number[]; reply_to?: number; reply_preview?: { msg_id: number; username: string; message: string; deleted?: boolean } }) => {
    const channelId = data.channel_id ?? firstChannelId.value
    chatMessages.value = [
      ...chatMessages.value,
      {
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
      },
    ]
    // Clear typing indicator for the sender
    if (data.sender_id && typingUsers.value[data.sender_id]) {
      const { [data.sender_id]: _, ...rest } = typingUsers.value
      typingUsers.value = rest
    }
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

  EventsOn('chat:message_edited', (data: { msg_id: number; message: string; ts: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    updated[idx] = { ...updated[idx], message: data.message, edited: true, editedTs: data.ts }
    chatMessages.value = updated
  })

  EventsOn('chat:message_deleted', (data: { msg_id: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    updated[idx] = { ...updated[idx], message: '', deleted: true }
    chatMessages.value = updated
  })

  EventsOn('chat:reaction_added', (data: { msg_id: number; emoji: string; id: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
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
      reactions.push({ emoji: data.emoji, user_ids: [data.id], count: 1 })
    }
    msg.reactions = reactions
    updated[idx] = msg
    chatMessages.value = updated
  })

  EventsOn('chat:reaction_removed', (data: { msg_id: number; emoji: string; id: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    const msg = { ...updated[idx] }
    let reactions = [...(msg.reactions ?? [])]
    const rxIdx = reactions.findIndex(r => r.emoji === data.emoji)
    if (rxIdx >= 0) {
      const rx = { ...reactions[rxIdx] }
      rx.user_ids = rx.user_ids.filter(id => id !== data.id)
      rx.count = rx.user_ids.length
      if (rx.count === 0) {
        reactions = reactions.filter((_, i) => i !== rxIdx)
      } else {
        reactions[rxIdx] = rx
      }
    }
    msg.reactions = reactions
    updated[idx] = msg
    chatMessages.value = updated
  })

  EventsOn('chat:user_typing', (data: { id: number; username: string; channel_id: number }) => {
    if (data.id === myID.value) return
    typingUsers.value = {
      ...typingUsers.value,
      [data.id]: { username: data.username, channelId: data.channel_id, expiresAt: Date.now() + 5000 },
    }
  })

  EventsOn('chat:message_pinned', (data: { msg_id: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    updated[idx] = { ...updated[idx], pinned: true }
    chatMessages.value = updated
  })

  EventsOn('chat:message_unpinned', (data: { msg_id: number }) => {
    const idx = chatMessages.value.findIndex(m => m.msgId === data.msg_id)
    if (idx === -1) return
    const updated = [...chatMessages.value]
    updated[idx] = { ...updated[idx], pinned: false }
    chatMessages.value = updated
  })

  // Clean up expired typing indicators every second.
  typingCleanupInterval = setInterval(() => {
    const now = Date.now()
    const next: typeof typingUsers.value = {}
    for (const [id, entry] of Object.entries(typingUsers.value)) {
      if (entry.expiresAt > now) next[Number(id)] = entry
    }
    typingUsers.value = next
  }, 1000)

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

  EventsOn('video:state', (data: { id: number; video_active: boolean; screen_share: boolean }) => {
    if (data.video_active) {
      videoStates.value = { ...videoStates.value, [data.id]: { active: true, screenShare: data.screen_share } }
    } else {
      const { [data.id]: _, ...rest } = videoStates.value
      videoStates.value = rest
    }
  })

  EventsOn('video:layers', (data: { id: number; layers: { quality: string; width: number; height: number; bitrate: number }[] }) => {
    const existing = videoStates.value[data.id]
    if (existing) {
      videoStates.value = { ...videoStates.value, [data.id]: { ...existing, layers: data.layers } }
    }
  })

  EventsOn('recording:state', (data: { channel_id: number; recording: boolean; started_by: string }) => {
    if (data.recording) {
      recordingChannels.value = { ...recordingChannels.value, [data.channel_id]: { recording: true, startedBy: data.started_by } }
    } else {
      const { [data.channel_id]: _, ...rest } = recordingChannels.value
      recordingChannels.value = rest
    }
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

  // Global keyboard shortcuts handler.
  window.addEventListener('keydown', handleGlobalShortcuts)

  // Register PTT key listeners.
  window.addEventListener('keydown', handlePTTKeyDown)
  window.addEventListener('keyup', handlePTTKeyUp)
  window.addEventListener('ptt-config-changed', ((e: CustomEvent) => {
    pttEnabled.value = e.detail.enabled
    pttKeyCode.value = e.detail.key
    pttKeyHeld = false
  }) as EventListener)

  // Apply saved audio settings before doing anything else so noise suppression,
  // AGC, and volume are active even if the user never opens the settings panel.
  await ApplyConfig()

  // Listen for density/system-messages preference changes from settings.
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

  if (auto.username) {
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
  } else if (globalUsername.value) {
    // Prefer reconnecting to the most recently used server after hot reload.
    const lastAddr = getLastConnectedAddr()
    if (lastAddr) {
      const ok = await connectToServer(lastAddr, globalUsername.value)
      if (ok) return
    }
    if (cfg.servers?.length) {
      // Fallback: auto-connect to the first saved server.
      await connectToServer(cfg.servers[0].addr, globalUsername.value)
    }
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncRouteFromHash)
  window.removeEventListener('keydown', handleGlobalShortcuts)
  window.removeEventListener('keydown', handlePTTKeyDown)
  window.removeEventListener('keyup', handlePTTKeyUp)
  EventsOff('connection:lost', 'user:list', 'user:joined', 'user:left', 'user:renamed', 'chat:message', 'chat:message_edited', 'chat:message_deleted', 'chat:link_preview', 'chat:reaction_added', 'chat:reaction_removed', 'chat:user_typing', 'chat:message_pinned', 'chat:message_unpinned', 'server:info', 'room:owner', 'user:me', 'connection:kicked', 'channel:list', 'channel:user_moved', 'audio:speaking', 'video:state', 'video:layers', 'recording:state', 'file:dropped')
  clearTimers()
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
