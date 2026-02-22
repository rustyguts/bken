import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ChannelChat from '../ChannelChat.vue'
import type { ChatMessage, Channel } from '../types'

function makeMsg(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 1,
    msgId: 100,
    senderId: 10,
    username: 'Alice',
    message: 'Hello world',
    ts: Date.now(),
    channelId: 0,
    ...overrides,
  }
}

describe('ChannelChat', () => {
  const baseProps = {
    messages: [] as ChatMessage[],
    channels: [] as Channel[],
    selectedChannelId: 0,
    myChannelId: 0,
    connected: true,
    unreadCounts: {} as Record<number, number>,
    myId: 10,
    ownerId: 0,
  }

  it('mounts without errors', () => {
    const w = mount(ChannelChat, { props: baseProps })
    expect(w.exists()).toBe(true)
  })

  it('shows empty state when no messages', () => {
    const w = mount(ChannelChat, { props: baseProps })
    expect(w.text()).toContain('No messages in this channel yet')
  })

  it('shows disconnected message when not connected', () => {
    const w = mount(ChannelChat, { props: { ...baseProps, connected: false } })
    expect(w.text()).toContain('Connect to a server to start chatting')
  })

  it('renders messages', () => {
    const messages = [makeMsg({ message: 'Test message' })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('Alice')
    expect(w.text()).toContain('Test message')
  })

  it('renders system messages', () => {
    const messages = [makeMsg({ system: true, message: 'Alice joined the server', username: '' })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages, showSystemMessages: true } })
    expect(w.text()).toContain('Alice joined the server')
  })

  it('hides system messages when showSystemMessages is false', () => {
    const messages = [makeMsg({ system: true, message: 'Alice joined the server', username: '' })]
    const w = mount(ChannelChat, {
      props: { ...baseProps, messages, showSystemMessages: false },
    })
    expect(w.text()).not.toContain('Alice joined the server')
  })

  it('shows deleted message indicator', () => {
    const messages = [makeMsg({ deleted: true, message: '' })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('message deleted')
  })

  it('shows edited indicator', () => {
    const messages = [makeMsg({ edited: true })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('(edited)')
  })

  it('renders selected channel name in header', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Dev' },
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, channels, selectedChannelId: 2 } })
    expect(w.find('header h2').text()).toContain('# Dev')
  })

  it('updates selected channel name when selectedChannelId changes', async () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Dev' },
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, channels, selectedChannelId: 1 } })
    expect(w.find('header h2').text()).toContain('# General')

    await w.setProps({ selectedChannelId: 2 })
    expect(w.find('header h2').text()).toContain('# Dev')
  })

  it('does not render channel selector tabs in header', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Dev' },
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, channels } })
    const hasChannelTabButton = w
      .findAll('header button')
      .some(btn => btn.text().includes('General') || btn.text().includes('Dev'))
    expect(hasChannelTabButton).toBe(false)
  })

  it('filters messages by selected channel', () => {
    const messages = [
      makeMsg({ id: 1, channelId: 0, message: 'Lobby msg' }),
      makeMsg({ id: 2, channelId: 1, message: 'General msg' }),
    ]
    const w = mount(ChannelChat, {
      props: { ...baseProps, messages, selectedChannelId: 1 },
    })
    expect(w.text()).not.toContain('Lobby msg')
    expect(w.text()).toContain('General msg')
  })

  it('emits send when Enter is pressed in the input', async () => {
    const w = mount(ChannelChat, { props: baseProps })
    const input = w.find('input[type="text"][maxlength="1024"]')
    await input.setValue('Hello')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('send')).toEqual([['Hello']])
  })

  it('does not emit send for empty input', async () => {
    const w = mount(ChannelChat, { props: baseProps })
    const input = w.find('input[type="text"][maxlength="1024"]')
    await input.setValue('  ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('send')).toBeUndefined()
  })

  it('clears input after sending', async () => {
    const w = mount(ChannelChat, { props: baseProps })
    const input = w.find('input[type="text"][maxlength="1024"]')
    await input.setValue('Hello')
    await input.trigger('keydown', { key: 'Enter' })
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('disables input when not connected', () => {
    const w = mount(ChannelChat, { props: { ...baseProps, connected: false } })
    const input = w.find('input[type="text"][maxlength="1024"]')
    expect(input.attributes('disabled')).toBeDefined()
  })

  it('shows placeholder with channel name', () => {
    const w = mount(ChannelChat, { props: baseProps })
    const input = w.find('input[type="text"][maxlength="1024"]')
    expect(input.attributes('placeholder')).toContain('Message #General')
  })

  it('renders reactions on a message', () => {
    const messages = [
      makeMsg({
        reactions: [{ emoji: '👍', user_ids: [1, 2], count: 2 }],
      }),
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('👍')
    expect(w.text()).toContain('2')
  })

  it('renders file attachment link', () => {
    const messages = [
      makeMsg({
        fileUrl: 'http://example.com/file.pdf',
        fileName: 'report.pdf',
        fileSize: 1024,
      }),
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('report.pdf')
    expect(w.text()).toContain('1.0 KB')
  })

  it('renders image preview for image files', () => {
    const messages = [
      makeMsg({
        fileUrl: 'http://example.com/photo.jpg',
        fileName: 'photo.jpg',
        fileSize: 2048,
      }),
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    const img = w.find('img[alt="photo.jpg"]')
    expect(img.exists()).toBe(true)
  })

  it('shows pinned badge on pinned messages', () => {
    const messages = [makeMsg({ pinned: true })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('pinned')
  })

  it('shows pinned messages button when there are pinned messages', () => {
    const messages = [makeMsg({ pinned: true })]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    const pinnedBtn = w.findAll('button').find(b => b.attributes('title') === 'Pinned messages')
    expect(pinnedBtn).toBeDefined()
  })

  it('shows search button', () => {
    const w = mount(ChannelChat, { props: baseProps })
    const searchBtn = w.findAll('button').find(b => b.attributes('title') === 'Search messages')
    expect(searchBtn).toBeDefined()
  })

  it('opens search bar when search button is clicked', async () => {
    const w = mount(ChannelChat, { props: baseProps })
    const searchBtn = w.findAll('button').find(b => b.attributes('title') === 'Search messages')
    await searchBtn!.trigger('click')
    const searchInput = w.find('input[placeholder="Search messages..."]')
    expect(searchInput.exists()).toBe(true)
  })

  it('renders link preview', () => {
    const messages = [
      makeMsg({
        linkPreview: {
          url: 'http://example.com',
          title: 'Example Site',
          description: 'A test page',
          image: '',
          siteName: 'Example',
        },
      }),
    ]
    const w = mount(ChannelChat, { props: { ...baseProps, messages } })
    expect(w.text()).toContain('Example Site')
    expect(w.text()).toContain('A test page')
  })

  it('shows typing indicator when users are typing', () => {
    const w = mount(ChannelChat, {
      props: {
        ...baseProps,
        typingUsers: { 2: { username: 'Bob', channelId: 0, expiresAt: Date.now() + 5000 } },
      },
    })
    expect(w.text()).toContain('Bob is typing...')
  })

  it('shows multiple typing users text', () => {
    const w = mount(ChannelChat, {
      props: {
        ...baseProps,
        typingUsers: {
          2: { username: 'Bob', channelId: 0, expiresAt: Date.now() + 5000 },
          3: { username: 'Carol', channelId: 0, expiresAt: Date.now() + 5000 },
        },
      },
    })
    expect(w.text()).toContain('Bob and Carol are typing...')
  })

  it('emits uploadFile when upload button is clicked', async () => {
    const w = mount(ChannelChat, { props: baseProps })
    const uploadBtn = w.findAll('button').find(b => b.attributes('title') === 'Upload file')
    await uploadBtn!.trigger('click')
    expect(w.emitted('uploadFile')).toHaveLength(1)
  })

  it('highlights @mention for current user', () => {
    const messages = [
      makeMsg({
        message: 'Hey @TestUser check this',
        mentions: [10],
      }),
    ]
    const w = mount(ChannelChat, {
      props: {
        ...baseProps,
        messages,
        users: [{ id: 10, username: 'TestUser' }],
      },
    })
    // The mention highlight adds a bg-warning class container
    const mentionArticle = w.find('.bg-warning\\/10')
    expect(mentionArticle.exists()).toBe(true)
  })

  it('respects compact density', () => {
    const messages = [makeMsg()]
    const w = mount(ChannelChat, {
      props: { ...baseProps, messages, messageDensity: 'compact' as const },
    })
    // Compact mode uses space-y-0 class on the messages container
    const container = w.find('.space-y-0')
    expect(container.exists()).toBe(true)
  })

  it('respects comfortable density with avatar', () => {
    const messages = [makeMsg()]
    const w = mount(ChannelChat, {
      props: { ...baseProps, messages, messageDensity: 'comfortable' as const },
    })
    // Comfortable mode uses space-y-2 class
    const container = w.find('.space-y-2')
    expect(container.exists()).toBe(true)
  })
})
