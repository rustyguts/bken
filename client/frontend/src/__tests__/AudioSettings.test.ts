import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AudioSettings from '../AudioSettings.vue'

describe('AudioSettings', () => {
  it('mounts without errors', async () => {
    const w = mount(AudioSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('shows header when showHeader is true', async () => {
    const w = mount(AudioSettings, { props: { showHeader: true } })
    await flushPromises()
    expect(w.text()).toContain('Settings')
  })

  it('hides header when showHeader is false', async () => {
    const w = mount(AudioSettings, { props: { showHeader: false } })
    await flushPromises()
    const header = w.find('h2')
    expect(header.exists()).toBe(false)
  })

  it('renders AudioDeviceSettings child', async () => {
    const w = mount(AudioSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'AudioDeviceSettings' }).exists()).toBe(true)
  })

  it('renders VoiceProcessing child', async () => {
    const w = mount(AudioSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'VoiceProcessing' }).exists()).toBe(true)
  })

  it('renders ThemePicker child', async () => {
    const w = mount(AudioSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'ThemePicker' }).exists()).toBe(true)
  })
})
