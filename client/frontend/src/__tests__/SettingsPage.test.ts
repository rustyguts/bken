import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsPage from '../SettingsPage.vue'

describe('SettingsPage', () => {
  it('mounts without errors', () => {
    const w = mount(SettingsPage)
    expect(w.exists()).toBe(true)
  })

  it('renders the Settings heading and all sections', () => {
    const w = mount(SettingsPage)
    expect(w.text()).toContain('Settings')
    expect(w.text()).toContain('Audio')
    expect(w.text()).toContain('Appearance')
    expect(w.text()).toContain('Keybinds')
    expect(w.text()).toContain('About')
  })

  it('defaults to audio section', () => {
    const w = mount(SettingsPage)
    const activeTab = w.find('[role="tab"][aria-selected="true"]')
    expect(activeTab.text()).toContain('Audio')
  })

  it('switches tabs on click', async () => {
    const w = mount(SettingsPage)
    const tabs = w.findAll('[role="tab"]')

    await tabs[1].trigger('click')
    expect(tabs[1].attributes('aria-selected')).toBe('true')

    await tabs[2].trigger('click')
    expect(tabs[2].attributes('aria-selected')).toBe('true')

    await tabs[3].trigger('click')
    expect(tabs[3].attributes('aria-selected')).toBe('true')
  })

  it('emits back when back button is clicked', async () => {
    const w = mount(SettingsPage)
    const backBtn = w.find('[aria-label="Back to room"]')
    await backBtn.trigger('click')
    expect(w.emitted('back')).toHaveLength(1)
  })

  it('renders AudioDeviceSettings and VoiceProcessing in audio section', () => {
    const w = mount(SettingsPage)
    expect(w.findComponent({ name: 'AudioDeviceSettings' }).exists()).toBe(true)
    expect(w.findComponent({ name: 'VoiceProcessing' }).exists()).toBe(true)
  })
})
