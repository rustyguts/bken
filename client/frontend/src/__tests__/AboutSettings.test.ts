import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AboutSettings from '../AboutSettings.vue'

describe('AboutSettings', () => {
  it('mounts without errors', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders about and build information headings', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('About')
    expect(w.text()).toContain('Build Information')
  })

  it('renders key build info labels', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('Git Commit')
    expect(w.text()).toContain('Go Version')
    expect(w.text()).toContain('Browser Engine')
    expect(w.text()).toContain('Runtime')
  })

  it('does not include ThemePicker', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'ThemePicker' }).exists()).toBe(false)
  })
})
