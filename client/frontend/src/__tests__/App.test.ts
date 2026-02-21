import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import App from '../App.vue'
import { emitWailsEvent, getGoMock } from './setup'

describe('App', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    window.location.hash = '#/'
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts without errors', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders TitleBar', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.findComponent({ name: 'TitleBar' }).exists()).toBe(true)
  })

  it('renders ChannelView by default', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.findComponent({ name: 'ChannelView' }).exists()).toBe(true)
  })

  it('does not render SettingsPage by default', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.findComponent({ name: 'SettingsPage' }).exists()).toBe(false)
  })

  it('navigates to settings when hash changes', async () => {
    const w = mount(App)
    await flushPromises()
    window.location.hash = '#/settings'
    window.dispatchEvent(new HashChangeEvent('hashchange'))
    await flushPromises()
    expect(w.findComponent({ name: 'SettingsPage' }).exists()).toBe(true)
  })

  it('navigates back to channel from settings', async () => {
    window.location.hash = '#/settings'
    const w = mount(App)
    await flushPromises()
    window.location.hash = '#/'
    window.dispatchEvent(new HashChangeEvent('hashchange'))
    await flushPromises()
    expect(w.findComponent({ name: 'ChannelView' }).exists()).toBe(true)
  })

  it('calls ApplyConfig on mount', async () => {
    const go = getGoMock()
    mount(App)
    await flushPromises()
    expect(go.ApplyConfig).toHaveBeenCalled()
  })

  it('loads config and sets globalUsername', async () => {
    const go = getGoMock()
    go.GetConfig.mockResolvedValue({
      username: 'Alice',
      theme: 'dark',
      servers: [],
      input_device_id: -1,
      output_device_id: -1,
      volume: 1,
      audio_bitrate_kbps: 32,
      noise_enabled: false,
      noise_level: 0,
      aec_enabled: false,
      agc_enabled: false,
      agc_level: 0,
      ptt_enabled: false,
      ptt_key: 'Backquote',
    })
    const w = mount(App)
    await flushPromises()
    // The ChannelView component should receive globalUsername prop
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('globalUsername')).toBe('Alice')
  })

  it('generates username when config has no username', async () => {
    const go = getGoMock()
    go.GetConfig.mockResolvedValue({
      username: '',
      theme: 'dark',
      servers: [],
      input_device_id: -1,
      output_device_id: -1,
      volume: 1,
      audio_bitrate_kbps: 32,
      noise_enabled: false,
      noise_level: 0,
      aec_enabled: false,
      agc_enabled: false,
      agc_level: 0,
      ptt_enabled: false,
      ptt_key: 'Backquote',
    })
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    // Should have generated a User-XXXX style name
    expect(channel.props('globalUsername')).toMatch(/^User-[0-9a-f]{4}$/)
    expect(go.SaveConfig).toHaveBeenCalled()
  })

  it('does not show ReconnectBanner by default', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.findComponent({ name: 'ReconnectBanner' }).exists()).toBe(false)
  })

  it('does not show KeyboardShortcuts by default', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.findComponent({ name: 'KeyboardShortcuts' }).exists()).toBe(false)
  })

  it('has app-grid layout', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.find('.app-grid').exists()).toBe(true)
  })

  it('handles user:list event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:list', [
      { id: 1, username: 'Alice', channel_id: 0 },
      { id: 2, username: 'Bob', channel_id: 1 },
    ])
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('users')).toHaveLength(2)
    expect(channel.props('userChannels')).toEqual({ 1: 0, 2: 1 })
  })

  it('handles user:joined event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:list', [{ id: 1, username: 'Alice' }])
    emitWailsEvent('user:joined', { id: 2, username: 'Bob' })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('users')).toHaveLength(2)
  })

  it('handles user:left event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:list', [{ id: 1, username: 'Alice' }, { id: 2, username: 'Bob' }])
    emitWailsEvent('user:left', { id: 2 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('users')).toHaveLength(1)
  })

  it('handles server:info event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('server:info', { name: 'Test Server' })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('serverName')).toBe('Test Server')
  })

  it('handles channel:owner event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('channel:owner', { owner_id: 42 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('ownerId')).toBe(42)
  })

  it('handles user:me event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:me', { id: 7 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('myId')).toBe(7)
  })

  it('handles chat:message event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice',
      message: 'Hello!',
      ts: Date.now(),
      channel_id: 0,
      msg_id: 1,
      sender_id: 1,
    })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('chatMessages')).toHaveLength(1)
    expect(channel.props('chatMessages')[0].message).toBe('Hello!')
  })

  it('handles channel:list event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('channel:list', [{ id: 1, name: 'General' }, { id: 2, name: 'Music' }])
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('channels')).toHaveLength(2)
  })

  it('handles channel:user_moved event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:list', [{ id: 1, username: 'Alice', channel_id: 0 }])
    emitWailsEvent('channel:user_moved', { user_id: 1, channel_id: 5 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('userChannels')[1]).toBe(5)
  })

  it('handles video:state event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('video:state', { id: 1, video_active: true, screen_share: false })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('videoStates')[1]).toEqual({ active: true, screenShare: false })
  })

  it('removes video state when deactivated', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('video:state', { id: 1, video_active: true, screen_share: false })
    emitWailsEvent('video:state', { id: 1, video_active: false, screen_share: false })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('videoStates')[1]).toBeUndefined()
  })

  it('handles recording:state event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('recording:state', { channel_id: 1, recording: true, started_by: 'Admin' })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('recordingChannels')[1]).toEqual({ recording: true, startedBy: 'Admin' })
  })

  it('handles connection:kicked event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('connection:kicked')
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('connected')).toBe(false)
    expect(channel.props('connectError')).toContain('Disconnected by server owner')
  })

  it('handles chat:message_edited event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'Original', ts: Date.now(),
      channel_id: 0, msg_id: 100, sender_id: 1,
    })
    emitWailsEvent('chat:message_edited', { msg_id: 100, message: 'Edited text', ts: Date.now() })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 100)
    expect(msg.message).toBe('Edited text')
    expect(msg.edited).toBe(true)
  })

  it('handles chat:message_deleted event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'To delete', ts: Date.now(),
      channel_id: 0, msg_id: 200, sender_id: 1,
    })
    emitWailsEvent('chat:message_deleted', { msg_id: 200 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 200)
    expect(msg.deleted).toBe(true)
    expect(msg.message).toBe('')
  })

  it('handles chat:reaction_added event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'React to me', ts: Date.now(),
      channel_id: 0, msg_id: 300, sender_id: 1,
    })
    emitWailsEvent('chat:reaction_added', { msg_id: 300, emoji: '👍', id: 2 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 300)
    expect(msg.reactions).toHaveLength(1)
    expect(msg.reactions[0].emoji).toBe('👍')
    expect(msg.reactions[0].count).toBe(1)
  })

  it('handles chat:reaction_removed event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'React', ts: Date.now(),
      channel_id: 0, msg_id: 400, sender_id: 1,
    })
    emitWailsEvent('chat:reaction_added', { msg_id: 400, emoji: '👍', id: 2 })
    emitWailsEvent('chat:reaction_removed', { msg_id: 400, emoji: '👍', id: 2 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 400)
    expect(msg.reactions).toHaveLength(0)
  })

  it('handles chat:user_typing event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:me', { id: 1 })
    emitWailsEvent('chat:user_typing', { id: 2, username: 'Bob', channel_id: 0 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('typingUsers')[2]).toBeDefined()
    expect(channel.props('typingUsers')[2].username).toBe('Bob')
  })

  it('ignores typing event from self', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:me', { id: 1 })
    emitWailsEvent('chat:user_typing', { id: 1, username: 'Me', channel_id: 0 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('typingUsers')[1]).toBeUndefined()
  })

  it('handles user:renamed event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('user:list', [{ id: 1, username: 'Alice' }])
    emitWailsEvent('user:renamed', { id: 1, username: 'Alice2' })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('users')[0].username).toBe('Alice2')
  })

  it('handles chat:message_pinned event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'Pin me', ts: Date.now(),
      channel_id: 0, msg_id: 500, sender_id: 1,
    })
    emitWailsEvent('chat:message_pinned', { msg_id: 500 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 500)
    expect(msg.pinned).toBe(true)
  })

  it('handles chat:message_unpinned event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'Pin me', ts: Date.now(),
      channel_id: 0, msg_id: 600, sender_id: 1,
    })
    emitWailsEvent('chat:message_pinned', { msg_id: 600 })
    emitWailsEvent('chat:message_unpinned', { msg_id: 600 })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 600)
    expect(msg.pinned).toBe(false)
  })

  it('increments unread count for unseen channel', async () => {
    const w = mount(App)
    await flushPromises()
    // User is viewing channel 0; message arrives in channel 5
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'Off-channel', ts: Date.now(),
      channel_id: 5, msg_id: 700, sender_id: 1,
    })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    expect(channel.props('unreadCounts')[5]).toBe(1)
  })

  it('calls Connect when ChannelView emits connect', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('connect', { username: 'Alice', addr: 'localhost:4433' })
    await flushPromises()
    expect(go.Connect).toHaveBeenCalledWith('localhost:4433', 'Alice')
  })

  it('keeps active server state when selectServer fails', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })

    channel.vm.$emit('connect', { username: 'Alice', addr: 'localhost:4433' })
    await flushPromises()
    emitWailsEvent('channel:list', [{ id: 1, name: 'General' }])
    await flushPromises()

    go.Connect.mockResolvedValueOnce('server not connected')
    channel.vm.$emit('selectServer', 'offline.example:4433')
    await flushPromises()

    const updatedChannelView = w.findComponent({ name: 'ChannelView' })
    expect(updatedChannelView.props('connectedAddr')).toBe('localhost:4433')
    expect(updatedChannelView.props('channels')).toEqual([{ id: 1, name: 'General' }])
    expect(updatedChannelView.props('connectError')).toContain('server not connected')
  })

  it('calls Disconnect when ChannelView emits disconnect', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('connect', { username: 'Alice', addr: 'localhost:4433' })
    await flushPromises()
    channel.vm.$emit('disconnect')
    await flushPromises()
    expect(go.Disconnect).toHaveBeenCalled()
  })

  it('clears voiceConnected even when DisconnectVoice fails', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })

    channel.vm.$emit('activateChannel', { addr: 'localhost:8080', channelID: 1 })
    await flushPromises()
    expect(channel.props('voiceConnected')).toBe(true)

    go.DisconnectVoice.mockResolvedValueOnce('control websocket write failed')
    channel.vm.$emit('disconnectVoice')
    await flushPromises()

    // Voice state must be cleared even on error — audio is already stopped
    expect(channel.props('voiceConnected')).toBe(false)
    expect(channel.props('connectError')).toContain('control websocket write failed')
  })

  it('handles sendChat event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('sendChat', 'Hello!')
    await flushPromises()
    expect(go.SendChat).toHaveBeenCalledWith('Hello!')
  })

  it('handles sendChannelChat event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('sendChannelChat', 5, 'Hello channel!')
    await flushPromises()
    expect(go.SendChannelChat).toHaveBeenCalledWith(5, 'Hello channel!')
  })

  it('handles createChannel event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('createChannel', 'New Ch')
    await flushPromises()
    expect(go.CreateChannel).toHaveBeenCalledWith('New Ch')
  })

  it('handles editMessage event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('editMessage', 10, 'updated')
    await flushPromises()
    expect(go.EditMessage).toHaveBeenCalledWith(10, 'updated')
  })

  it('handles deleteMessage event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('deleteMessage', 10)
    await flushPromises()
    expect(go.DeleteMessage).toHaveBeenCalledWith(10)
  })

  it('handles kickUser event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('kickUser', 99)
    await flushPromises()
    expect(go.KickUser).toHaveBeenCalledWith(99)
  })

  it('handles startVideo event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('startVideo')
    await flushPromises()
    expect(go.StartVideo).toHaveBeenCalled()
  })

  it('handles stopVideo event', async () => {
    const go = getGoMock()
    const w = mount(App)
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    channel.vm.$emit('stopVideo')
    await flushPromises()
    expect(go.StopVideo).toHaveBeenCalled()
  })

  it('handles link preview event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('chat:message', {
      username: 'Alice', message: 'Check this', ts: Date.now(),
      channel_id: 0, msg_id: 800, sender_id: 1,
    })
    emitWailsEvent('chat:link_preview', {
      msg_id: 800, channel_id: 0, url: 'http://example.com',
      title: 'Example', description: 'A page', image: '', site_name: 'Ex',
    })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const msg = channel.props('chatMessages').find((m: any) => m.msgId === 800)
    expect(msg.linkPreview).toBeDefined()
    expect(msg.linkPreview.title).toBe('Example')
  })

  it('handles video:layers event', async () => {
    const w = mount(App)
    await flushPromises()
    emitWailsEvent('video:state', { id: 1, video_active: true, screen_share: false })
    emitWailsEvent('video:layers', {
      id: 1,
      layers: [{ quality: 'high', width: 1920, height: 1080, bitrate: 2000 }],
    })
    await flushPromises()
    const channel = w.findComponent({ name: 'ChannelView' })
    const vs = channel.props('videoStates')[1]
    expect(vs.layers).toHaveLength(1)
    expect(vs.layers[0].quality).toBe('high')
  })
})
