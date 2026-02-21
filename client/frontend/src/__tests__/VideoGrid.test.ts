import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import VideoGrid from '../VideoGrid.vue'
import type { User, VideoState } from '../types'

describe('VideoGrid', () => {
  const users: User[] = [
    { id: 1, username: 'Alice' },
    { id: 2, username: 'Bob' },
    { id: 3, username: 'Carol' },
  ]

  const baseProps = {
    users,
    videoStates: {} as Record<number, VideoState>,
    myId: 1,
    spotlightId: null as number | null,
  }

  it('mounts without errors', () => {
    const w = mount(VideoGrid, { props: baseProps })
    expect(w.exists()).toBe(true)
  })

  it('renders nothing when no user has active video', () => {
    const w = mount(VideoGrid, { props: baseProps })
    expect(w.find('.video-grid-container').exists()).toBe(false)
  })

  it('renders grid when users have active video', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: {
          1: { active: true, screenShare: false },
          2: { active: true, screenShare: false },
        },
      },
    })
    expect(w.find('.video-grid-container').exists()).toBe(true)
  })

  it('shows username on video tile', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 1: { active: true, screenShare: false } },
      },
    })
    expect(w.text()).toContain('Alice')
  })

  it('shows "You" badge for current user', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 1: { active: true, screenShare: false } },
      },
    })
    expect(w.text()).toContain('You')
  })

  it('shows "Screen" badge for screen share', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 2: { active: true, screenShare: true } },
      },
    })
    expect(w.text()).toContain('Screen')
  })

  it('applies grid-cols-1 for single video', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 1: { active: true, screenShare: false } },
      },
    })
    expect(w.find('.grid-cols-1').exists()).toBe(true)
  })

  it('applies grid-cols-2 for two videos', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: {
          1: { active: true, screenShare: false },
          2: { active: true, screenShare: false },
        },
      },
    })
    expect(w.find('.grid-cols-2').exists()).toBe(true)
  })

  it('emits spotlight on double-click', async () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 2: { active: true, screenShare: false } },
      },
    })
    const tile = w.find('.video-tile')
    await tile.trigger('dblclick')
    expect(w.emitted('spotlight')).toEqual([[2]])
  })

  it('enters spotlight mode for specific user', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: {
          1: { active: true, screenShare: false },
          2: { active: true, screenShare: false },
        },
        spotlightId: 2,
      },
    })
    // In spotlight mode, only spotlighted user's tile should show
    expect(w.text()).toContain('Bob')
  })

  it('exits spotlight on double-click when already spotlighted', async () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 2: { active: true, screenShare: false } },
        spotlightId: 2,
      },
    })
    const tile = w.find('.video-tile')
    await tile.trigger('dblclick')
    expect(w.emitted('spotlight')).toEqual([[null]])
  })

  it('shows exit spotlight button in spotlight mode', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 2: { active: true, screenShare: false } },
        spotlightId: 2,
      },
    })
    const exitBtn = w.findAll('button').find(b => b.attributes('title') === 'Exit spotlight')
    expect(exitBtn).toBeDefined()
  })

  it('shows user initials in video placeholder', () => {
    const w = mount(VideoGrid, {
      props: {
        ...baseProps,
        videoStates: { 2: { active: true, screenShare: false } },
      },
    })
    expect(w.text()).toContain('B') // Bob's initial
  })
})
