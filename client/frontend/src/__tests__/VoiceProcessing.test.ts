import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import VoiceProcessing from '../VoiceProcessing.vue'

describe('VoiceProcessing', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts without errors', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Voice Processing heading', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Voice Processing')
  })

  it('renders Echo Cancellation toggle', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Echo Cancellation')
    const aecToggle = w.find('[aria-label="Toggle echo cancellation"]')
    expect(aecToggle.exists()).toBe(true)
  })

  it('renders Noise Suppression toggle', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Noise Suppression')
    const noiseToggle = w.find('[aria-label="Toggle noise suppression"]')
    expect(noiseToggle.exists()).toBe(true)
  })

  it('renders Auto Gain Control toggle', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Auto Gain Control')
    const agcToggle = w.find('[aria-label="Toggle automatic gain control"]')
    expect(agcToggle.exists()).toBe(true)
  })

  it('renders Voice Activity Detection toggle', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Voice Activity Detection')
    const vadToggle = w.find('[aria-label="Toggle voice activity detection"]')
    expect(vadToggle.exists()).toBe(true)
  })

  it('renders Noise Gate toggle', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Noise Gate')
    const gateToggle = w.find('[aria-label="Toggle noise gate"]')
    expect(gateToggle.exists()).toBe(true)
  })

  it('renders Input Level meter', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Input Level')
  })

  it('renders Notification Volume section', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Notification Volume')
  })

  it('shows strength slider for Noise Suppression', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Strength')
  })

  it('shows target level slider for AGC', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Target Level')
  })

  it('shows sensitivity slider for VAD', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Sensitivity')
  })

  it('shows threshold slider for Noise Gate', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Threshold')
  })
})
