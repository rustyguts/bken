import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import VoiceProcessing from '../VoiceProcessing.vue'

describe('VoiceProcessing', () => {
  it('mounts without errors', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders friendly voice processing heading', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()
    expect(w.text()).toContain('Voice Enhancements')
    expect(w.text()).toContain('Help Others Hear You Clearly')
    expect(w.text()).not.toContain('Live')
  })

  it('renders only the three processing toggles', async () => {
    const w = mount(VoiceProcessing)
    await flushPromises()

    expect(w.find('[aria-label="Toggle echo cancellation"]').exists()).toBe(true)
    expect(w.find('[aria-label="Toggle noise suppression"]').exists()).toBe(true)
    expect(w.find('[aria-label="Toggle volume normalization"]').exists()).toBe(true)

    expect(w.text()).not.toContain('Voice Activity Detection')
    expect(w.text()).not.toContain('Noise Gate')
    expect(w.text()).not.toContain('Notification Volume')
    expect(w.text()).not.toContain('Input Level')
  })
})
