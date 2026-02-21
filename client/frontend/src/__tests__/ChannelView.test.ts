import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ChannelView from '../ChannelView.vue'
import type { User, ChatMessage, Channel, VideoState } from '../types'

describe('ChannelView', () => {
  const baseProps = {
    connected: true,
    voiceConnected: true,
    reconnecting: false,
    connectedAddr: 'localhost:8080',
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
    servers: [{ name: 'Local Dev', addr: 'localhost:8080' }],
    userVoiceFlags: {} as Record<number, { muted: boolean; deafened: boolean }>,
  }

  it('mounts without errors', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Sidebar component', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'Sidebar' }).exists()).toBe(true)
  })

  it('renders ServerChannels component', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'ServerChannels' }).exists()).toBe(true)
  })

  it('renders ChannelChat component', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'ChannelChat' }).exists()).toBe(true)
  })

  it('renders VideoGrid component', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.findComponent({ name: 'VideoGrid' }).exists()).toBe(true)
  })

  it('applies grid layout class', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    expect(w.find('.grid').exists()).toBe(true)
  })

  it('emits selectServer when triggered from sidebar', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sidebar = w.findComponent({ name: 'Sidebar' })
    sidebar.vm.$emit('selectServer', 'other:8080')
    expect(w.emitted('selectServer')).toEqual([['other:8080']])
  })

  it('emits disconnectVoice from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('leave-voice')
    await flushPromises()
    expect(w.emitted('disconnectVoice')).toBeDefined()
  })

  it('emits sendChannelChat for all messages including channel 0', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChat' })
    chat.vm.$emit('send', 'hello')
    await flushPromises()
    // ChannelView always uses sendChannelChat with selectedChannelId (defaults to 0)
    expect(w.emitted('sendChannelChat')).toEqual([[0, 'hello']])
  })

  it('emits sendChannelChat for non-lobby messages', async () => {
    const w = mount(ChannelView, {
      props: {
        ...baseProps,
        channels: [{ id: 1, name: 'General' }],
        userChannels: { 1: 1 },
      },
    })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChat' })
    chat.vm.$emit('send', 'channel msg')
    await flushPromises()
    expect(w.emitted('sendChannelChat')).toEqual([[1, 'channel msg']])
  })

  it('emits openSettings from Sidebar', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sidebar = w.findComponent({ name: 'Sidebar' })
    sidebar.vm.$emit('openSettings')
    await flushPromises()
    expect(w.emitted('openSettings')).toBeDefined()
  })

  it('emits createChannel from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('createChannel', 'New Ch')
    await flushPromises()
    expect(w.emitted('createChannel')).toEqual([['New Ch']])
  })

  it('emits deleteChannel from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('deleteChannel', 5)
    await flushPromises()
    expect(w.emitted('deleteChannel')).toEqual([[5]])
  })

  it('emits kickUser from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('kickUser', 42)
    await flushPromises()
    expect(w.emitted('kickUser')).toEqual([[42]])
  })

  it('emits editMessage from channel chat', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChat' })
    chat.vm.$emit('editMessage', 10, 'updated text')
    await flushPromises()
    expect(w.emitted('editMessage')).toEqual([[10, 'updated text']])
  })

  it('emits deleteMessage from channel chat', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChat' })
    chat.vm.$emit('deleteMessage', 10)
    await flushPromises()
    expect(w.emitted('deleteMessage')).toEqual([[10]])
  })

  it('emits addReaction from channel chat', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const chat = w.findComponent({ name: 'ChannelChat' })
    chat.vm.$emit('addReaction', 10, '👍')
    await flushPromises()
    expect(w.emitted('addReaction')).toEqual([[10, '👍']])
  })

  it('emits startVideo from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('video-toggle')
    await flushPromises()
    expect(w.emitted('startVideo')).toBeDefined()
  })

  it('emits stopVideo when video is already active', async () => {
    const w = mount(ChannelView, {
      props: {
        ...baseProps,
        videoStates: { 1: { active: true, screenShare: false } },
      },
    })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('video-toggle')
    await flushPromises()
    expect(w.emitted('stopVideo')).toBeDefined()
  })

  it('emits startScreenShare from ServerChannels', async () => {
    const w = mount(ChannelView, { props: baseProps })
    await flushPromises()
    const sc = w.findComponent({ name: 'ServerChannels' })
    sc.vm.$emit('screen-share-toggle')
    await flushPromises()
    expect(w.emitted('startScreenShare')).toBeDefined()
  })
})
