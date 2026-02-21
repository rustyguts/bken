import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AppearanceSettings from '../AppearanceSettings.vue'

describe('AppearanceSettings', () => {
  it('mounts without errors', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders ThemePicker', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.findComponent({ name: 'ThemePicker' }).exists()).toBe(true)
  })

  it('renders Message Density heading', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.text()).toContain('Message Density')
  })

  it('renders all density options', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.text()).toContain('Compact')
    expect(w.text()).toContain('Default')
    expect(w.text()).toContain('Comfortable')
  })

  it('renders density descriptions', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.text()).toContain('No avatars')
    expect(w.text()).toContain('name above message')
    expect(w.text()).toContain('Larger avatar')
  })

  it('highlights the default density option', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    const defaultBtn = w.findAll('button').find(b => b.text().includes('Default') && b.text().includes('name above'))
    expect(defaultBtn?.classes()).toContain('border-primary')
  })

  it('changes density on click', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    const compactBtn = w.findAll('button').find(b => b.text().includes('Compact'))
    await compactBtn!.trigger('click')
    await flushPromises()
    expect(compactBtn!.classes()).toContain('border-primary')
  })

  it('renders Chat section with system messages toggle', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    expect(w.text()).toContain('Chat')
    expect(w.text()).toContain('Show system messages')
  })

  it('has system messages toggle checked by default', async () => {
    const w = mount(AppearanceSettings)
    await flushPromises()
    const toggle = w.find('input[type="checkbox"]')
    expect((toggle.element as HTMLInputElement).checked).toBe(true)
  })
})
