import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ThemePicker from '../ThemePicker.vue'

describe('ThemePicker', () => {
  it('mounts without errors', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders Appearance heading', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    expect(w.text()).toContain('Appearance')
  })

  it('renders System mode button', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    expect(w.text()).toContain('System (follow OS)')
  })

  it('renders theme buttons', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    expect(w.text()).toContain('Dark')
    expect(w.text()).toContain('Light')
    expect(w.text()).toContain('Dracula')
    expect(w.text()).toContain('Synthwave')
  })

  it('applies active style to current theme', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    // Default theme is "dark"
    const darkBtn = w.findAll('button[role="radio"]').find(b => b.text() === 'Dark')
    expect(darkBtn?.classes()).toContain('btn-primary')
  })

  it('has radiogroup role', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    const radiogroup = w.find('[role="radiogroup"]')
    expect(radiogroup.exists()).toBe(true)
  })

  it('theme buttons have aria-checked', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    const darkBtn = w.findAll('button[role="radio"]').find(b => b.text() === 'Dark')
    expect(darkBtn?.attributes('aria-checked')).toBe('true')
  })

  it('non-active theme has aria-checked=false', async () => {
    const w = mount(ThemePicker)
    await flushPromises()
    const lightBtn = w.findAll('button[role="radio"]').find(b => b.text() === 'Light')
    expect(lightBtn?.attributes('aria-checked')).toBe('false')
  })
})
