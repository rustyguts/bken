import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AudioDeviceSettings from '../AudioDeviceSettings.vue'

describe('AudioDeviceSettings', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts without errors', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Input heading', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    expect(w.text()).toContain('Input')
  })

  it('renders Output heading', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    expect(w.text()).toContain('Output')
  })

  it('renders microphone select with Default option', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    const micSelect = w.find('[aria-label="Microphone device"]')
    expect(micSelect.exists()).toBe(true)
    expect(micSelect.text()).toContain('Default')
  })

  it('renders speaker select with Default option', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    const spkSelect = w.find('[aria-label="Speaker device"]')
    expect(spkSelect.exists()).toBe(true)
    expect(spkSelect.text()).toContain('Default')
  })

  it('renders Test Mic button', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    const testBtn = w.findAll('button').find(b => b.text().includes('Test Mic'))
    expect(testBtn).toBeDefined()
  })

  it('renders volume slider', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    const slider = w.find('[aria-label="Playback volume"]')
    expect(slider.exists()).toBe(true)
  })

  it('shows Volume label', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    expect(w.text()).toContain('Volume')
  })

  it('renders mic level meter and bitrate slider', async () => {
    const w = mount(AudioDeviceSettings)
    await flushPromises()
    expect(w.text()).toContain('Mic level')
    expect(w.find('[aria-label="Audio bitrate"]').exists()).toBe(true)
  })
})
