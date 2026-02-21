import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import KeybindsSettings from '../KeybindsSettings.vue'

describe('KeybindsSettings', () => {
  it('mounts without errors', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Key Bindings heading', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    expect(w.text()).toContain('Key Bindings')
  })

  it('renders Push to Talk toggle', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    expect(w.text()).toContain('Push to Talk')
    const toggle = w.find('[aria-label="Toggle push-to-talk"]')
    expect(toggle.exists()).toBe(true)
  })

  it('shows the default PTT key (backtick)', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    expect(w.text()).toContain('`')
  })

  it('renders rebind button', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    // The button shows the key label
    const btn = w.findAll('button').find(b => b.text().includes('`'))
    expect(btn).toBeDefined()
  })

  it('shows rebind instruction text', async () => {
    const w = mount(KeybindsSettings)
    await flushPromises()
    expect(w.text()).toContain('press any key to rebind')
  })
})
