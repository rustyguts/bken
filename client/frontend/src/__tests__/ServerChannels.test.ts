import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ServerChannels from '../ServerChannels.vue'
import type { Channel, User } from '../types'

const stubs = { global: { stubs: { teleport: true } } }

describe('ServerChannels', () => {
  const baseProps = {
    channels: [{ id: 1, name: 'General' }] as Channel[],
    users: [
      { id: 1, username: 'Alice' },
      { id: 2, username: 'Bob' },
    ] as User[],
    userChannels: { 1: 1, 2: 1 } as Record<number, number>,
    myId: 1,
    selectedChannelId: 1,
    serverName: 'Test Server',
    speakingUsers: new Set<number>(),
    connectError: '',
    isOwner: false,
    ownerId: 0,
    unreadCounts: {} as Record<number, number>,
    recordingChannels: {} as Record<number, { recording: boolean; startedBy: string }>,
    voiceConnected: false,
    videoActive: false,
    screenSharing: false,
    muted: false,
    deafened: false,
    userVoiceFlags: {} as Record<number, { muted: boolean; deafened: boolean }>,
  }

  it('mounts without errors', () => {
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    expect(w.exists()).toBe(true)
  })

  it('renders server name in header', () => {
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    expect(w.text()).toContain('Test Server')
  })

  it('defaults to "Server" when no server name', () => {
    const w = mount(ServerChannels, { props: { ...baseProps, serverName: '' }, ...stubs })
    expect(w.text()).toContain('Server')
  })

  it('renders named channels', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Music' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 2 } },
      ...stubs,
    })
    expect(w.text()).toContain('General')
    expect(w.text()).toContain('Music')
  })

  it('shows user count badge for each channel', () => {
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    // Two users in channel 1
    expect(w.text()).toContain('2')
  })

  it('shows user avatars with initials', () => {
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    expect(w.text()).toContain('A') // Alice
    expect(w.text()).toContain('B') // Bob
  })

  it('hides user count badge for empty channels', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Empty' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 1 } },
      ...stubs,
    })
    // Empty channel should not show a user count badge.
    expect(w.text()).toContain('Empty')
    const badges = w.findAll('.badge-ghost')
    const emptyCount = badges.some(b => b.text().trim() === '0')
    expect(emptyCount).toBe(false)
  })

  it('highlights speaking users with success styling', () => {
    const w = mount(ServerChannels, {
      props: { ...baseProps, speakingUsers: new Set([1]) },
      ...stubs,
    })
    const speakingAvatar = w.find('.bg-success\\/20')
    expect(speakingAvatar.exists()).toBe(true)
  })

  it('emits select when a channel row is clicked', async () => {
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    const channelLink = w.find('a.group')
    await channelLink.trigger('click')
    expect(w.emitted('select')).toBeDefined()
  })

  it('shows connect error when present', () => {
    const w = mount(ServerChannels, {
      props: { ...baseProps, connectError: 'Connection failed' },
      ...stubs,
    })
    expect(w.text()).toContain('Connection failed')
  })

  it('keeps channel text basic and shows green audio icon when channel has users', () => {
    // myId=1 is in channel 1 (via userChannels), but channel text should remain basic.
    const w = mount(ServerChannels, { props: baseProps, ...stubs })
    const connectedLink = w.find('a.font-semibold')
    expect(connectedLink.exists()).toBe(false)
    const speakerIcon = w.find('svg.text-success')
    expect(speakerIcon.exists()).toBe(true)
  })

  it('shows "Join" button for channels user is not in', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Music' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 1 } },
      ...stubs,
    })
    // User is in channel 1, not in channel 2 -> Music should have a Join button
    const joinBtn = w.findAll('button').find(b => b.text().includes('Join'))
    expect(joinBtn).toBeDefined()
  })

  it('shows create channel button when owner', () => {
    const w = mount(ServerChannels, { props: { ...baseProps, isOwner: true }, ...stubs })
    const createBtn = w.findAll('button').find(b => b.text().includes('Create Channel'))
    expect(createBtn).toBeDefined()
  })

  it('shows create channel button for admin users', () => {
    const users: User[] = [
      { id: 1, username: 'Alice', role: 'ADMIN' },
      { id: 2, username: 'Bob', role: 'USER' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, users, isOwner: false },
      ...stubs,
    })
    const createBtn = w.findAll('button').find(b => b.text().includes('Create Channel'))
    expect(createBtn).toBeDefined()
  })

  it('shows server admin settings icon for admin users', () => {
    const users: User[] = [
      { id: 1, username: 'Alice', role: 'ADMIN' },
      { id: 2, username: 'Bob', role: 'USER' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, users, isOwner: false },
      ...stubs,
    })
    const settingsBtn = w.findAll('button').find(b => b.text().includes('Admin Server Settings'))
    expect(settingsBtn).toBeDefined()
  })

  it('opens server admin modal and closes it', async () => {
    const users: User[] = [
      { id: 1, username: 'Alice', role: 'ADMIN' },
      { id: 2, username: 'Bob', role: 'USER' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, users, isOwner: false },
      ...stubs,
    })
    const settingsBtn = w.findAll('button').find(b => b.text().includes('Admin Server Settings'))
    expect(settingsBtn).toBeDefined()
    await settingsBtn!.trigger('click')
    expect(w.text()).toContain('Server Admin Settings')

    const cancelBtn = w.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeDefined()
    await cancelBtn!.trigger('click')
    expect(w.text()).toContain('General')
  })

  it('shows server admin settings in dev mode for local server even without role', () => {
    const users: User[] = [
      { id: 1, username: 'Alice' },
      { id: 2, username: 'Bob' },
    ]
    const w = mount(ServerChannels, {
      props: {
        ...baseProps,
        users,
        isOwner: false,
        connectedAddr: 'localhost:8080',
      },
      ...stubs,
    })
    const settingsBtn = w.findAll('button').find(b => b.text().includes('Admin Server Settings'))
    expect(settingsBtn).toBeDefined()
  })

  it('hides create channel button when not owner', () => {
    const w = mount(ServerChannels, { props: { ...baseProps, isOwner: false }, ...stubs })
    const createBtn = w.findAll('button').find(b => b.text().includes('Create Channel'))
    expect(createBtn).toBeUndefined()
  })

  it('shows unread badge on channel', () => {
    const w = mount(ServerChannels, {
      props: { ...baseProps, unreadCounts: { 1: 3 } },
      ...stubs,
    })
    expect(w.text()).toContain('3')
  })

  it('shows recording indicator', () => {
    const w = mount(ServerChannels, {
      props: {
        ...baseProps,
        recordingChannels: { 1: { recording: true, startedBy: 'Admin' } },
      },
      ...stubs,
    })
    expect(w.text()).toContain('REC')
  })

  it('emits join when Join button is clicked', async () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Music' },
    ]
    const w = mount(ServerChannels, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 1 } },
      ...stubs,
    })
    const joinBtn = w.findAll('button[title="Connect to voice"]').find(b => b.attributes('aria-hidden') !== 'true')
    expect(joinBtn).toBeDefined()
    await joinBtn!.trigger('click')
    expect(w.emitted('join')).toBeDefined()
    expect(w.emitted('join')![0]).toEqual([2])
  })

  it('shows no channels message when channels list is empty', () => {
    const w = mount(ServerChannels, {
      props: { ...baseProps, channels: [], userChannels: {} },
      ...stubs,
    })
    // The main channel list ul has the flex-1 class; its li count should be 0
    const channelList = w.find('ul.flex-1')
    const listItems = channelList.findAll('li')
    expect(listItems.length).toBe(0)
  })
})
