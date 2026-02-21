import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import KeyboardShortcuts from '../KeyboardShortcuts.vue'

const mountOpts = { global: { stubs: { teleport: true } } }

describe('KeyboardShortcuts', () => {
  it('mounts without errors', () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    expect(w.exists()).toBe(true)
  })

  it('renders the title', () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    expect(w.text()).toContain('Keyboard Shortcuts')
  })

  it('displays all shortcut entries', () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    const expected = ['Ctrl + /', '?', 'M', 'D', 'Ctrl + Shift + M', 'Ctrl + K', 'Escape']
    for (const key of expected) {
      expect(w.text()).toContain(key)
    }
  })

  it('displays descriptions for each shortcut', () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    expect(w.text()).toContain('Toggle mute')
    expect(w.text()).toContain('Toggle deafen')
    expect(w.text()).toContain('Close modals')
  })

  it('emits close when the close button is clicked', async () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    const closeBtn = w.find('button')
    await closeBtn.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('emits close when clicking the backdrop', async () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    const backdrop = w.find('.fixed.inset-0')
    await backdrop.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('renders kbd elements for key combinations', () => {
    const w = mount(KeyboardShortcuts, mountOpts)
    const kbds = w.findAll('kbd')
    expect(kbds.length).toBeGreaterThanOrEqual(7)
  })
})
