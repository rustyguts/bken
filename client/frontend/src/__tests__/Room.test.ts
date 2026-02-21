import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import Room from '../Room.vue'
import type { User, ChatMessage, Channel, VideoState } from '../types'

describe('Room', () => {
  const baseProps = {
    connected: true,
    voiceConnected: true,
    reconnecting: false,
    connectedAddr: 'localhost:8443',
    connectError: '',
    startupAddr: '',
    globalUsername: 'TestUser',
    serverName: 'Dev Server',
    users: [{ id: 1, username: 'TestUser' }] as User[],
    chatMessages: [] as ChatMessage[],
    ownerId: 1,
    myId: 1,
    channels: [] as Channel[],
    userChannels: { 1: 0 } as Record<number, number>,
    speakingUsers: new Set<number>(),
    unreadCounts: {} as Record<number, number>,
    videoStates: {} as Record<number, VideoState>,
    recordingChannels: {} as Record<number, { recording: boolean; startedBy: string }>,
    typingUsers: {} as Record<number, { username: string; channelId: number; expiresAt: number }>,
    messageDensity: 'default' as const,
    showSystemMessages: true,
  }

  it('mounts without errors', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Sidebar component', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'Sidebar' }).exists()).toBe(true)
  })

  it('renders ServerChannels component', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'ServerChannels' }).exists()).toBe(true)
  })

  it('renders ChannelChatroom component', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'ChannelChatroom' }).exists()).toBe(true)
  })

  it('renders UserControls component', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'UserControls' }).exists()).toBe(true)
  })

  it('renders VideoGrid component', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'VideoGrid' }).exists()).toBe(true)
  })

  it('applies room-grid layout class', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    expect(w.find('.room-grid').exists()).toBe(true)
  })

  it('emits selectServer when triggered from sidebar', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const sidebar = w.findComponent({ name: 'Sidebar' })
    sidebar.vm.$emit('selectServer', 'other:8443')
    expect(w.emitted('selectServer')).toEqual([['other:8443']])
  })

  it('emits disconnectVoice from UserControls', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const controls = w.findComponent({ name: 'UserControls' })
    controls.vm.$emit('leaveVoice')
    await flushPromises()
    expect(w.emitted('disconnectVoice')).toBeDefined()
  })

  it('emits sendChannelChat for all messages including channel 0', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChatroom' })
    chat.vm.$emit('send', 'hello')
    await flushPromises()
    // Room always uses sendChannelChat with selectedChannelId (defaults to 0)
    expect(w.emitted('sendChannelChat')).toEqual([[0, 'hello']])
  })

  it('emits sendChannelChat for non-lobby messages', async () => {
    const w = mount(Room, {
      props: {
        ...baseProps,
        channels: [{ id: 1, name: 'General' }],
        userChannels: { 1: 1 },
      },
    })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChatroom' })
    chat.vm.$emit('send', 'channel msg')
    await flushPromises()
    expect(w.emitted('sendChannelChat')).toEqual([[1, 'channel msg']])
  })

  it('emits openSettings from UserControls', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const controls = w.findComponent({ name: 'UserControls' })
    controls.vm.$emit('openSettings')
    await flushPromises()
    expect(w.emitted('openSettings')).toBeDefined()
  })

  it('emits createChannel from ServerChannels', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('createChannel', 'New Ch')
    await flushPromises()
    expect(w.emitted('createChannel')).toEqual([['New Ch']])
  })

  it('emits deleteChannel from ServerChannels', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('deleteChannel', 5)
    await flushPromises()
    expect(w.emitted('deleteChannel')).toEqual([[5]])
  })

  it('emits kickUser from ServerChannels', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('kickUser', 42)
    await flushPromises()
    expect(w.emitted('kickUser')).toEqual([[42]])
  })

  it('emits editMessage from chatroom', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChatroom' })
    chat.vm.$emit('editMessage', 10, 'updated text')
    await flushPromises()
    expect(w.emitted('editMessage')).toEqual([[10, 'updated text']])
  })

  it('emits deleteMessage from chatroom', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChatroom' })
    chat.vm.$emit('deleteMessage', 10)
    await flushPromises()
    expect(w.emitted('deleteMessage')).toEqual([[10]])
  })

  it('emits addReaction from chatroom', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChatroom' })
    chat.vm.$emit('addReaction', 10, '👍')
    await flushPromises()
    expect(w.emitted('addReaction')).toEqual([[10, '👍']])
  })

  it('emits startVideo from UserControls', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const controls = w.findComponent({ name: 'UserControls' })
    controls.vm.$emit('videoToggle')
    await flushPromises()
    expect(w.emitted('startVideo')).toBeDefined()
  })

  it('emits stopVideo when video is already active', async () => {
    const w = mount(Room, {
      props: {
        ...baseProps,
        videoStates: { 1: { active: true, screenShare: false } },
      },
    })
    await flushPromises()
    const controls = w.findComponent({ name: 'UserControls' })
    controls.vm.$emit('videoToggle')
    await flushPromises()
    expect(w.emitted('stopVideo')).toBeDefined()
  })

  it('emits startScreenShare from UserControls', async () => {
    const w = mount(Room, { props: baseProps })
    await flushPromises()
    const controls = w.findComponent({ name: 'UserControls' })
    controls.vm.$emit('screenShareToggle')
    await flushPromises()
    expect(w.emitted('startScreenShare')).toBeDefined()
  })
})
