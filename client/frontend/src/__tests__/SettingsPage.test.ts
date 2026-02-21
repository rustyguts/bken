import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsPage from '../SettingsPage.vue'

describe('SettingsPage', () => {
  it('mounts without errors', () => {
    const w = mount(SettingsPage)
    expect(w.exists()).toBe(true)
  })

  it('renders the Settings heading', () => {
    const w = mount(SettingsPage)
    expect(w.text()).toContain('Settings')
  })

  it('renders all tabs', () => {
    const w = mount(SettingsPage)
    expect(w.text()).toContain('Audio')
    expect(w.text()).toContain('Appearance')
    expect(w.text()).toContain('Keybinds')
    expect(w.text()).toContain('About')
  })

  it('defaults to audio tab', () => {
    const w = mount(SettingsPage)
    const activeTab = w.find('[aria-selected="true"]')
    expect(activeTab.text()).toBe('Audio')
  })

  it('switches to appearance tab on click', async () => {
    const w = mount(SettingsPage)
    const tabs = w.findAll('[role="tab"]')
    const appearanceTab = tabs.find(t => t.text() === 'Appearance')
    await appearanceTab!.trigger('click')
    expect(appearanceTab!.classes()).toContain('tab-active')
  })

  it('switches to keybinds tab on click', async () => {
    const w = mount(SettingsPage)
    const tabs = w.findAll('[role="tab"]')
    const keybindsTab = tabs.find(t => t.text() === 'Keybinds')
    await keybindsTab!.trigger('click')
    expect(keybindsTab!.classes()).toContain('tab-active')
  })

  it('switches to about tab on click', async () => {
    const w = mount(SettingsPage)
    const tabs = w.findAll('[role="tab"]')
    const aboutTab = tabs.find(t => t.text() === 'About')
    await aboutTab!.trigger('click')
    expect(aboutTab!.classes()).toContain('tab-active')
  })

  it('emits back when back button is clicked', async () => {
    const w = mount(SettingsPage)
    const backBtn = w.find('[aria-label="Back to room"]')
    await backBtn.trigger('click')
    expect(w.emitted('back')).toHaveLength(1)
  })

  it('renders AudioDeviceSettings in audio tab', () => {
    const w = mount(SettingsPage)
    expect(w.findComponent({ name: 'AudioDeviceSettings' }).exists()).toBe(true)
  })

  it('renders VoiceProcessing in audio tab', () => {
    const w = mount(SettingsPage)
    expect(w.findComponent({ name: 'VoiceProcessing' }).exists()).toBe(true)
  })
})
