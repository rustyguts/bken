import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import UserProfilePopup from '../UserProfilePopup.vue'

const stubs = { global: { stubs: { teleport: true } } }

describe('UserProfilePopup', () => {
  const baseProps = {
    user: { id: 1, username: 'Alice' },
    x: 100,
    y: 200,
    isOwner: false,
    myId: 2,
    ownerUserId: 3,
    userChannels: { 1: 0, 2: 0 } as Record<number, number>,
    speakingUsers: new Set<number>(),
  }

  function m(propsOverride = {}) {
    return mount(UserProfilePopup, { props: { ...baseProps, ...propsOverride }, ...stubs })
  }

  it('mounts without errors', () => {
    expect(m().exists()).toBe(true)
  })

  it('displays the username', () => {
    expect(m().text()).toContain('Alice')
  })

  it('displays user ID', () => {
    expect(m().text()).toContain('ID: 1')
  })

  it('shows initials in the avatar', () => {
    expect(m().text()).toContain('A')
  })

  it('shows "User" role for non-owner', () => {
    expect(m().text()).toContain('User')
  })

  it('shows "Owner" role when user is the owner', () => {
    expect(m({ ownerUserId: 1 }).text()).toContain('Owner')
  })

  it('shows "In voice" status when user is in a channel', () => {
    expect(m().text()).toContain('In voice')
  })

  it('shows "Speaking" status when user is speaking', () => {
    expect(m({ speakingUsers: new Set([1]) }).text()).toContain('Speaking')
  })

  it('shows "In voice" status when user is in a non-zero channel', () => {
    expect(m({ userChannels: { 1: 5 } }).text()).toContain('In voice')
  })

  it('does NOT show kick button when not owner', () => {
    expect(m().text()).not.toContain('Kick')
  })

  it('shows kick button when owner and target is not self', () => {
    expect(m({ isOwner: true, myId: 2 }).text()).toContain('Kick')
  })

  it('does NOT show kick button when owner but target is self', () => {
    expect(m({ isOwner: true, myId: 1 }).text()).not.toContain('Kick')
  })

  it('emits kick and close when kick is clicked', async () => {
    const w = m({ isOwner: true, myId: 2 })
    const kickBtn = w.findAll('button').find(b => b.text().includes('Kick'))
    expect(kickBtn).toBeDefined()
    await kickBtn!.trigger('click')
    expect(w.emitted('kick')).toEqual([[1]])
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('emits close on backdrop click', async () => {
    const w = m()
    const backdrop = w.find('.fixed.inset-0')
    await backdrop.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })
})
