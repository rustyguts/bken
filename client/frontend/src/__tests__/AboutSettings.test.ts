import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AboutSettings from '../AboutSettings.vue'

describe('AboutSettings', () => {
  it('mounts without errors', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders About heading', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('About')
  })

  it('renders app name', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('bken')
  })

  it('renders app description', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('LAN voice chat application')
  })

  it('mentions the tech stack', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('Wails')
    expect(w.text()).toContain('Vue 3')
    expect(w.text()).toContain('Go')
  })

  it('mentions audio codec', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('Opus')
    expect(w.text()).toContain('PortAudio')
  })

  it('mentions transport protocol', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.text()).toContain('WebTransport')
  })

  it('includes ThemePicker component', async () => {
    const w = mount(AboutSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'ThemePicker' }).exists()).toBe(true)
  })
})
