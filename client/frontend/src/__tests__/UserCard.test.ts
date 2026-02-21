import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import UserCard from '../UserCard.vue'

describe('UserCard', () => {
  const baseProps = {
    user: { id: 1, username: 'Alice' },
    speaking: false,
    muted: false,
    canKick: false,
  }

  it('mounts without errors', () => {
    const w = mount(UserCard, { props: baseProps })
    expect(w.exists()).toBe(true)
  })

  it('displays the username', () => {
    const w = mount(UserCard, { props: baseProps })
    expect(w.text()).toContain('Alice')
  })

  it('shows user initial in the avatar', () => {
    const w = mount(UserCard, { props: baseProps })
    expect(w.text()).toContain('A')
  })

  it('shows ? for empty username', () => {
    const w = mount(UserCard, { props: { ...baseProps, user: { id: 1, username: '' } } })
    expect(w.text()).toContain('?')
  })

  it('applies speaking ring style when speaking and not muted', () => {
    const w = mount(UserCard, { props: { ...baseProps, speaking: true } })
    const avatar = w.find('.ring-success')
    expect(avatar.exists()).toBe(true)
  })

  it('does not apply speaking ring when muted even if speaking', () => {
    const w = mount(UserCard, { props: { ...baseProps, speaking: true, muted: true } })
    const avatar = w.find('.ring-success')
    expect(avatar.exists()).toBe(false)
  })

  it('shows muted badge when muted', () => {
    const w = mount(UserCard, { props: { ...baseProps, muted: true } })
    const badge = w.find('.badge-error')
    expect(badge.exists()).toBe(true)
  })

  it('applies opacity when muted', () => {
    const w = mount(UserCard, { props: { ...baseProps, muted: true } })
    const opacity = w.find('.opacity-40')
    expect(opacity.exists()).toBe(true)
  })

  it('shows Mute button text', () => {
    const w = mount(UserCard, { props: baseProps })
    expect(w.text()).toContain('Mute')
  })

  it('shows Unmute button text when muted', () => {
    const w = mount(UserCard, { props: { ...baseProps, muted: true } })
    expect(w.text()).toContain('Unmute')
  })

  it('emits toggleMute with user id on click', async () => {
    const w = mount(UserCard, { props: baseProps })
    const muteBtn = w.findAll('button').find(b => b.text().includes('Mute'))
    await muteBtn!.trigger('click')
    expect(w.emitted('toggleMute')).toEqual([[1]])
  })

  it('shows kick button when canKick is true', () => {
    const w = mount(UserCard, { props: { ...baseProps, canKick: true } })
    expect(w.text()).toContain('Kick')
  })

  it('hides kick button when canKick is false', () => {
    const w = mount(UserCard, { props: { ...baseProps, canKick: false } })
    expect(w.text()).not.toContain('Kick')
  })

  it('emits kick with user id when kick button is clicked', async () => {
    const w = mount(UserCard, { props: { ...baseProps, canKick: true } })
    const kickBtn = w.findAll('button').find(b => b.text().includes('Kick'))
    await kickBtn!.trigger('click')
    expect(w.emitted('kick')).toEqual([[1]])
  })

  it('has proper aria-label with username', () => {
    const w = mount(UserCard, { props: baseProps })
    const el = w.find('[aria-label]')
    expect(el.attributes('aria-label')).toContain('Alice')
  })

  it('has aria-label mentioning speaking state', () => {
    const w = mount(UserCard, { props: { ...baseProps, speaking: true } })
    const el = w.find('[aria-label]')
    expect(el.attributes('aria-label')).toContain('speaking')
  })

  it('has aria-label mentioning muted state', () => {
    const w = mount(UserCard, { props: { ...baseProps, muted: true } })
    const el = w.find('[aria-label]')
    expect(el.attributes('aria-label')).toContain('muted')
  })

  it('has role=listitem', () => {
    const w = mount(UserCard, { props: baseProps })
    const el = w.find('[role="listitem"]')
    expect(el.exists()).toBe(true)
  })
})
