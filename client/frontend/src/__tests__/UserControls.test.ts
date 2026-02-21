import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import UserControls from '../UserControls.vue'

describe('UserControls', () => {
  const baseProps = {
    username: 'TestUser',
    muted: false,
    deafened: false,
    connected: true,
    voiceConnected: true,
  }

  it('mounts without errors', () => {
    const w = mount(UserControls, { props: baseProps })
    expect(w.exists()).toBe(true)
  })

  it('displays the user initial in the avatar', () => {
    const w = mount(UserControls, { props: baseProps })
    // "T" for TestUser
    const avatar = w.find('.rounded-full')
    expect(avatar.text()).toBe('T')
  })

  it('shows mute button with correct aria-pressed', () => {
    const w = mount(UserControls, { props: { ...baseProps, muted: true } })
    const muteBtn = w.find('[aria-pressed="true"]')
    expect(muteBtn.exists()).toBe(true)
  })

  it('mute button is disabled when not voice connected', () => {
    const w = mount(UserControls, { props: { ...baseProps, voiceConnected: false } })
    const muteBtn = w.findAll('button').find(b => b.attributes('title') === 'Mute')
    expect(muteBtn?.attributes('disabled')).toBeDefined()
  })

  it('emits mute-toggle when mute button is clicked', async () => {
    const w = mount(UserControls, { props: baseProps })
    const muteBtn = w.findAll('button').find(b => b.attributes('title') === 'Mute')
    await muteBtn!.trigger('click')
    expect(w.emitted('mute-toggle')).toHaveLength(1)
  })

  it('emits deafen-toggle when deafen button is clicked', async () => {
    const w = mount(UserControls, { props: baseProps })
    const deafenBtn = w.findAll('button').find(b => b.attributes('title') === 'Deafen')
    await deafenBtn!.trigger('click')
    expect(w.emitted('deafen-toggle')).toHaveLength(1)
  })

  it('applies text-error class when muted', () => {
    const w = mount(UserControls, { props: { ...baseProps, muted: true } })
    const muteBtn = w.findAll('button').find(b => b.attributes('title') === 'Unmute')
    expect(muteBtn?.classes()).toContain('text-error')
  })

  it('applies text-error class when deafened', () => {
    const w = mount(UserControls, { props: { ...baseProps, deafened: true } })
    const deafenBtn = w.findAll('button').find(b => b.attributes('title') === 'Undeafen')
    expect(deafenBtn?.classes()).toContain('text-error')
  })

  it('emits open-settings when settings button is clicked', async () => {
    const w = mount(UserControls, { props: baseProps })
    const settingsBtn = w.findAll('button').find(b => b.attributes('title') === 'Open Settings')
    await settingsBtn!.trigger('click')
    expect(w.emitted('open-settings')).toHaveLength(1)
  })

  it('renders MetricsBar when voice connected', () => {
    const w = mount(UserControls, { props: baseProps })
    // MetricsBar is a child component
    expect(w.findComponent({ name: 'MetricsBar' }).exists()).toBe(true)
  })

  it('does not render MetricsBar when voice not connected', () => {
    const w = mount(UserControls, { props: { ...baseProps, voiceConnected: false } })
    expect(w.findComponent({ name: 'MetricsBar' }).exists()).toBe(false)
  })
})
