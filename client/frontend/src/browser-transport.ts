/**
 * Browser-native transport for running the bken frontend outside of Wails.
 * Speaks the bken WebSocket protocol directly from the browser.
 */

/* eslint-disable @typescript-eslint/no-explicit-any */

type Listener = { cb: (...args: any[]) => void; remaining: number }

/**
 * Event bus matching the Wails runtime EventsOn/EventsOff API.
 */
export class BrowserEventBus {
  private listeners = new Map<string, Listener[]>()

  EventsOn(name: string, cb: (...args: any[]) => void): () => void {
    return this.onMultiple(name, cb, -1)
  }

  EventsOff(name: string, ...names: string[]): void {
    this.listeners.delete(name)
    for (const n of names) this.listeners.delete(n)
  }

  EventsOffAll(): void {
    this.listeners.clear()
  }

  EventsOnMultiple(name: string, cb: (...args: any[]) => void, maxCallbacks: number): () => void {
    return this.onMultiple(name, cb, maxCallbacks)
  }

  EventsOnce(name: string, cb: (...args: any[]) => void): () => void {
    return this.onMultiple(name, cb, 1)
  }

  EventsEmit(name: string, ...data: any[]): void {
    const list = this.listeners.get(name)
    if (!list) return
    const keep: Listener[] = []
    for (const l of list) {
      l.cb(...data)
      if (l.remaining > 0) l.remaining--
      if (l.remaining !== 0) keep.push(l)
    }
    this.listeners.set(name, keep)
  }

  private onMultiple(name: string, cb: (...args: any[]) => void, max: number): () => void {
    if (!this.listeners.has(name)) this.listeners.set(name, [])
    this.listeners.get(name)!.push({ cb, remaining: max })
    return () => this.EventsOff(name)
  }
}

/**
 * WebSocket transport speaking the bken server protocol.
 * Translates server string IDs to sequential local integers
 * (matching Go transport behaviour).
 */
export class BrowserTransport {
  readonly eventBus: BrowserEventBus
  private ws: WebSocket | null = null
  private selfId = ''
  private selfLocalId = 0
  private serverAddr = ''
  private idMap = new Map<string, number>()
  private nextId = 1

  constructor(eventBus: BrowserEventBus) {
    this.eventBus = eventBus
  }

  /** Map a server string ID (e.g. "u1") to a local integer. */
  private translateId(serverId: string): number {
    let local = this.idMap.get(serverId)
    if (local === undefined) {
      local = this.nextId++
      this.idMap.set(serverId, local)
    }
    return local
  }

  /**
   * Connect to a bken server. Opens WebSocket, performs the hello/snapshot
   * handshake, then sends connect_server. Returns '' on success, error string
   * on failure (matching Go bridge behaviour).
   */
  connect(addr: string, username: string): Promise<string> {
    return new Promise((resolve) => {
      try {
        this.serverAddr = addr
        this.idMap.clear()
        this.nextId = 1
        const wsUrl = `ws://${addr}/ws`
        this.ws = new WebSocket(wsUrl)

        this.ws.onopen = () => {
          this.ws!.send(JSON.stringify({ type: 'hello', username }))
        }

        this.ws.onerror = () => {
          resolve('WebSocket connection failed')
        }

        this.ws.onclose = () => {
          this.eventBus.EventsEmit('server:disconnected', {
            server_addr: this.serverAddr,
          })
        }

        let handshakeDone = false
        this.ws.onmessage = (event) => {
          let msg: any
          try {
            msg = JSON.parse(event.data)
          } catch {
            return
          }

          if (!handshakeDone && msg.type === 'snapshot') {
            handshakeDone = true
            this.handleSnapshot(msg)
            // Complete handshake with connect_server
            this.ws!.send(
              JSON.stringify({
                type: 'connect_server',
                server_id: this.serverAddr,
              }),
            )
            this.eventBus.EventsEmit('server:connected', {
              server_addr: this.serverAddr,
            })
            resolve('')
            return
          }

          if (handshakeDone) {
            this.handleMessage(msg)
          }
        }
      } catch (e: any) {
        resolve(e.message || 'Connection failed')
      }
    })
  }

  /** Close the WebSocket connection. */
  disconnect(): void {
    const addr = this.serverAddr
    if (this.ws) {
      this.ws.onclose = null // prevent duplicate disconnect event
      this.ws.close()
      this.ws = null
    }
    this.selfId = ''
    this.selfLocalId = 0
    this.serverAddr = ''
    this.idMap.clear()
    this.nextId = 1
    this.eventBus.EventsEmit('server:disconnected', { server_addr: addr })
  }

  /** Send join_voice for the given channel. */
  joinVoice(channelId: number): void {
    this.send({
      type: 'join_voice',
      server_id: this.serverAddr,
      channel_id: String(channelId),
    })
  }

  /** Send DisconnectVoice and locally emit channel:user_moved. */
  disconnectVoice(): void {
    this.send({ type: 'DisconnectVoice' })
    this.eventBus.EventsEmit('channel:user_moved', {
      user_id: this.selfLocalId,
      channel_id: 0,
    })
  }

  /** Create a channel with the given name. */
  createChannel(name: string): void {
    this.send({ type: 'create_channel', message: name })
  }

  /** Request the channel list from the server. */
  requestChannels(): void {
    this.send({ type: 'get_channels' })
  }

  /** Request server info. */
  requestServerInfo(): void {
    this.send({ type: 'get_server_info' })
  }

  /** Request message history for a channel. */
  requestMessages(channelId: number): void {
    this.send({ type: 'get_messages', channel_id: String(channelId) })
  }

  /** Send a text message (lobby chat). */
  sendChat(message: string): void {
    this.send({ type: 'send_text', message })
  }

  /** Send a text message to a specific channel. */
  sendChannelChat(channelId: number, message: string): void {
    this.send({ type: 'send_text', channel_id: String(channelId), message })
  }

  private send(msg: Record<string, any>): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  // --- Message handlers ---

  private handleSnapshot(msg: any): void {
    this.selfId = msg.self_id
    this.selfLocalId = this.translateId(msg.self_id)
    const users = (msg.users || []).map((u: any) => ({
      id: this.translateId(u.id),
      username: u.username,
      channel_id: u.voice ? parseInt(u.voice.channel_id, 10) || 0 : 0,
    }))

    this.eventBus.EventsEmit('user:list', users)
    this.eventBus.EventsEmit('user:me', { id: this.selfLocalId })

    // Emit voice flags for users already in voice
    for (const u of msg.users || []) {
      if (u.voice) {
        this.eventBus.EventsEmit('channel:user_voice_flags', {
          user_id: this.translateId(u.id),
          muted: !!u.voice.muted,
          deafened: !!u.voice.deafened,
        })
      }
    }
  }

  private handleMessage(msg: any): void {
    switch (msg.type) {
      case 'snapshot':
        this.handleSnapshot(msg)
        break

      case 'user_joined': {
        const user = msg.user || msg
        const localId = this.translateId(user.id)
        this.eventBus.EventsEmit('user:joined', {
          id: localId,
          username: user.username,
        })
        this.eventBus.EventsEmit('channel:user_moved', {
          user_id: localId,
          channel_id: 0,
        })
        break
      }

      case 'user_left': {
        const user = msg.user || msg
        const localId = this.translateId(user.id)
        this.eventBus.EventsEmit('user:left', { id: localId })
        break
      }

      case 'user_state': {
        const user = msg.user || msg
        const localId = this.translateId(user.id)
        let channelId = 0
        if (user.voice && user.voice.channel_id) {
          channelId = parseInt(user.voice.channel_id, 10) || 0
        }
        this.eventBus.EventsEmit('channel:user_moved', {
          user_id: localId,
          channel_id: channelId,
        })
        if (user.voice) {
          this.eventBus.EventsEmit('channel:user_voice_flags', {
            user_id: localId,
            muted: !!user.voice.muted,
            deafened: !!user.voice.deafened,
          })
        }
        break
      }

      case 'channel_list': {
        const channels = (msg.channels || []).map((ch: any) => ({
          id: ch.id,
          name: ch.name,
          max_users: ch.max_users || 0,
        }))
        this.eventBus.EventsEmit('channel:list', channels)
        break
      }

      case 'server_info':
        this.eventBus.EventsEmit('server:info', {
          name: msg.server_name || msg.name || '',
        })
        break

      case 'text_message': {
        const senderId = msg.user?.id
          ? this.translateId(msg.user.id)
          : 0
        const payload: Record<string, any> = {
          username: msg.user?.username || msg.username,
          message: msg.message,
          ts: msg.ts,
          channel_id: msg.channel_id
            ? parseInt(msg.channel_id, 10) || 0
            : 0,
          msg_id: msg.msg_id || 0,
          sender_id: senderId,
        }
        if (msg.file_id) {
          payload.file_id = msg.file_id
          payload.file_name = msg.file_name
          payload.file_size = msg.file_size
          payload.file_url = `/api/blobs/${msg.file_id}`
        }
        this.eventBus.EventsEmit('chat:message', payload)
        break
      }

      case 'reaction_added': {
        const localId = msg.user_id ? this.translateId(msg.user_id) : 0
        this.eventBus.EventsEmit('chat:reaction_added', {
          msg_id: msg.msg_id,
          emoji: msg.emoji,
          id: localId,
        })
        break
      }

      case 'reaction_removed': {
        const localId = msg.user_id ? this.translateId(msg.user_id) : 0
        this.eventBus.EventsEmit('chat:reaction_removed', {
          msg_id: msg.msg_id,
          emoji: msg.emoji,
          id: localId,
        })
        break
      }

      case 'message_history': {
        const channelId = msg.channel_id
          ? parseInt(msg.channel_id, 10) || 0
          : 0
        const messages = (msg.messages || []).map((m: any) => ({
          msg_id: m.msg_id,
          username: m.username,
          message: m.message,
          ts: m.ts,
          reactions: m.reactions?.map((rx: any) => ({
            emoji: rx.emoji,
            user_ids: (rx.user_ids || []).map((uid: string) => this.translateId(uid)),
            count: rx.count,
          })),
          file_id: m.file_id,
          file_name: m.file_name,
          file_size: m.file_size,
          file_url: m.file_id ? `/api/blobs/${m.file_id}` : undefined,
        }))
        this.eventBus.EventsEmit('chat:history', {
          channel_id: channelId,
          messages,
        })
        break
      }

      case 'pong':
        break

      case 'error':
        console.error('[bken] Server error:', msg.error || msg.message)
        break
    }
  }

  /**
   * Returns a bridge object matching the window.go.main.App API.
   * Used to install browser globals so wailsjs imports work transparently.
   */
  bridgeObject(): Record<string, (...args: any[]) => Promise<any>> {
    const self = this
    const defaultConfig = {
      theme: 'dark',
      theme_mode: 'manual',
      username: '',
      input_device_id: 0,
      output_device_id: 0,
      volume: 1,
      audio_bitrate_kbps: 32,
      noise_enabled: false,
      aec_enabled: false,
      agc_enabled: false,
      ptt_enabled: false,
      ptt_key: 'Backquote',
      servers: [{ name: 'Local Dev', addr: 'localhost:8080' }],
      message_density: 'default',
      show_system_messages: true,
    }

    return {
      // --- Transport methods ---
      Connect: (addr: string, username: string) =>
        self.connect(addr, username),
      Disconnect: () => {
        self.disconnect()
        return Promise.resolve()
      },
      DisconnectVoice: () => {
        self.disconnectVoice()
        return Promise.resolve('')
      },
      ConnectVoice: (channelID: number) => {
        self.joinVoice(channelID)
        return Promise.resolve('')
      },
      JoinChannel: (channelID: number) => {
        self.joinVoice(channelID)
        return Promise.resolve('')
      },
      CreateChannel: (name: string) => {
        self.createChannel(name)
        return Promise.resolve('')
      },
      RequestChannels: () => {
        self.requestChannels()
        return Promise.resolve('')
      },
      RequestServerInfo: () => {
        self.requestServerInfo()
        return Promise.resolve('')
      },
      RequestMessages: (channelID: number) => {
        self.requestMessages(channelID)
        return Promise.resolve('')
      },
      SendChat: (msg: string) => {
        self.sendChat(msg)
        return Promise.resolve('')
      },
      SendChannelChat: (channelID: number, msg: string) => {
        self.sendChannelChat(channelID, msg)
        return Promise.resolve('')
      },

      // --- Config (localStorage-backed) ---
      GetAutoLogin: () => Promise.resolve({ username: '', addr: '' }),
      GetConfig: () => {
        try {
          const stored = localStorage.getItem('bken_config')
          if (stored)
            return Promise.resolve({ ...defaultConfig, ...JSON.parse(stored) })
        } catch {
          /* ignore */
        }
        return Promise.resolve({ ...defaultConfig })
      },
      SaveConfig: (cfg: any) => {
        try {
          localStorage.setItem('bken_config', JSON.stringify(cfg))
        } catch {
          /* ignore */
        }
        return Promise.resolve()
      },
      ApplyConfig: () => Promise.resolve(),
      GetStartupAddr: () => Promise.resolve(''),
      GetBuildInfo: () =>
        Promise.resolve({
          commit: 'browser',
          build_time: new Date().toISOString(),
          go_version: 'n/a',
          goos: 'browser',
          goarch: 'wasm',
          dirty: false,
        }),

      // --- No-ops (audio/video/moderation not available in browser mode) ---
      SetMuted: () => Promise.resolve(),
      SetDeafened: () => Promise.resolve(),
      SetAEC: () => Promise.resolve(),
      SetAGC: () => Promise.resolve(),
      SetAudioBitrate: () => Promise.resolve(),
      GetAudioBitrate: () => Promise.resolve(32),
      GetInputLevel: () => Promise.resolve(0),
      SetNotificationVolume: () => Promise.resolve(),
      GetNotificationVolume: () => Promise.resolve(0.5),
      SetPTTMode: () => Promise.resolve(),
      PTTKeyDown: () => Promise.resolve(),
      PTTKeyUp: () => Promise.resolve(),
      MuteUser: () => Promise.resolve(),
      UnmuteUser: () => Promise.resolve(),
      GetMutedUsers: () => Promise.resolve([]),
      SetUserVolume: () => Promise.resolve(),
      GetUserVolume: () => Promise.resolve(1.0),
      KickUser: () => Promise.resolve(''),
      RenameServer: () => Promise.resolve(''),
      RenameUser: () => Promise.resolve(''),
      RenameChannel: () => Promise.resolve(''),
      DeleteChannel: () => Promise.resolve(''),
      MoveUserToChannel: () => Promise.resolve(''),
      UploadFile: (channelID: number) => {
        return new Promise<string>((resolve) => {
          const input = document.createElement('input')
          input.type = 'file'
          input.onchange = async () => {
            const file = input.files?.[0]
            if (!file) { resolve(''); return }
            const form = new FormData()
            form.append('file', file)
            try {
              const resp = await fetch(`http://${self.serverAddr}/api/upload`, { method: 'POST', body: form })
              if (!resp.ok) { resolve(`upload failed (${resp.status})`); return }
              const data = await resp.json()
              self.send({
                type: 'send_text',
                server_id: self.serverAddr,
                channel_id: String(channelID),
                message: '',
                file_id: data.id,
                file_name: data.original_name,
                file_size: data.size_bytes,
              })
              resolve('')
            } catch (e: any) {
              resolve(e.message || 'upload failed')
            }
          }
          input.click()
        })
      },
      UploadFileFromPath: () => Promise.resolve(''),
      EditMessage: () => Promise.resolve(''),
      DeleteMessage: () => Promise.resolve(''),
      AddReaction: (msgID: number, emoji: string) => {
        self.send({ type: 'add_reaction', msg_id: msgID, emoji })
        return Promise.resolve('')
      },
      RemoveReaction: (msgID: number, emoji: string) => {
        self.send({ type: 'remove_reaction', msg_id: msgID, emoji })
        return Promise.resolve('')
      },
      StartVideo: () => Promise.resolve(''),
      StopVideo: () => Promise.resolve(''),
      StartScreenShare: () => Promise.resolve(''),
      StopScreenShare: () => Promise.resolve(''),
      RequestVideoQuality: () => Promise.resolve(''),
      GetInputDevices: () => Promise.resolve([]),
      GetOutputDevices: () => Promise.resolve([]),
      GetMetrics: () =>
        Promise.resolve({ latency: 0, jitter: 0, loss: 0 }),
      SetInputDevice: () => Promise.resolve(),
      SetOutputDevice: () => Promise.resolve(),
      SetVolume: () => Promise.resolve(),
      StartTest: () => Promise.resolve(''),
      StopTest: () => Promise.resolve(),
      IsConnected: () =>
        Promise.resolve(
          self.ws !== null && self.ws.readyState === WebSocket.OPEN,
        ),
      SetNoiseSuppression: () => Promise.resolve(),
    }
  }
}
