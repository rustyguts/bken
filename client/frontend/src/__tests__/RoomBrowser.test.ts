import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import RoomBrowser from '../RoomBrowser.vue'
import type { User, Channel } from '../types'

describe('RoomBrowser', () => {
  const baseProps = {
    users: [
      { id: 1, username: 'Alice' },
      { id: 2, username: 'Bob' },
    ] as User[],
    speakingUsers: new Set<number>(),
    ownerId: 1,
    myId: 1,
    channels: [] as Channel[],
    userChannels: { 1: 0, 2: 0 } as Record<number, number>,
  }

  it('mounts without errors', () => {
    const w = mount(RoomBrowser, { props: baseProps })
    expect(w.exists()).toBe(true)
  })

  it('renders user cards', () => {
    const w = mount(RoomBrowser, { props: baseProps })
    expect(w.text()).toContain('Alice')
    expect(w.text()).toContain('Bob')
  })

  it('renders "No one else is here" when no users and no channels', () => {
    const w = mount(RoomBrowser, { props: { ...baseProps, users: [], channels: [] } })
    expect(w.text()).toContain('No one else is here')
  })

  it('renders channels', () => {
    const channels: Channel[] = [
      { id: 1, name: 'General' },
      { id: 2, name: 'Music' },
    ]
    const w = mount(RoomBrowser, { props: { ...baseProps, channels } })
    expect(w.text()).toContain('General')
    expect(w.text()).toContain('Music')
  })

  it('shows users in correct channel', () => {
    const channels: Channel[] = [{ id: 1, name: 'General' }]
    const w = mount(RoomBrowser, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 0 } },
    })
    expect(w.text()).toContain('Alice')
    expect(w.text()).toContain('Bob')
  })

  it('shows Lobby for users in channel 0', () => {
    const channels: Channel[] = [{ id: 1, name: 'General' }]
    const w = mount(RoomBrowser, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 0 } },
    })
    expect(w.text()).toContain('Lobby')
  })

  it('shows empty channel message', () => {
    const channels: Channel[] = [{ id: 1, name: 'Empty' }]
    const w = mount(RoomBrowser, {
      props: { ...baseProps, channels, userChannels: { 1: 0, 2: 0 } },
    })
    expect(w.text()).toContain('Empty')
    expect(w.text()).toContain('click to join')
  })

  it('channel button shows current channel indicator', () => {
    const channels: Channel[] = [{ id: 1, name: 'General' }]
    const w = mount(RoomBrowser, {
      props: { ...baseProps, channels, userChannels: { 1: 1, 2: 0 } },
    })
    // myId=1 is in channel 1, so General button should have active indicator
    expect(w.html()).toContain('text-primary')
  })

  it('shows Server fallback heading when no channels', () => {
    const w = mount(RoomBrowser, { props: baseProps })
    expect(w.text()).toContain('Server')
  })
})
