import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import MetricsBar from '../MetricsBar.vue'

describe('MetricsBar', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts without errors', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('defaults to compact mode', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    const el = w.find('[role="status"]')
    expect(el.exists()).toBe(true)
    expect(el.attributes('aria-label')).toBe('Connection quality')
  })

  it('shows "Connecting" quality by default in compact mode', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    expect(w.text()).toContain('Connecting')
  })

  it('shows --- for RTT when not connected in compact mode', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    expect(w.text()).toContain('---')
  })

  it('shows 0% for packet loss by default in compact mode', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    expect(w.text()).toContain('0%')
  })

  it('shows Opus codec label in compact mode', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    expect(w.text()).toContain('Opus')
  })

  it('renders expanded mode with detailed stats', async () => {
    const w = mount(MetricsBar, { props: { mode: 'expanded' } })
    await flushPromises()
    expect(w.text()).toContain('RTT')
    expect(w.text()).toContain('Packet Loss')
    expect(w.text()).toContain('Jitter')
    expect(w.text()).toContain('Bitrate')
    expect(w.text()).toContain('Status')
  })

  it('expanded mode has role=status with connection details label', async () => {
    const w = mount(MetricsBar, { props: { mode: 'expanded' } })
    await flushPromises()
    const el = w.find('[role="status"]')
    expect(el.exists()).toBe(true)
    expect(el.attributes('aria-label')).toBe('Connection details')
  })

  it('does not show drops count when drops are 0', async () => {
    const w = mount(MetricsBar)
    await flushPromises()
    // Drops should not be visible by default (0 drops)
    expect(w.text()).not.toContain('d')
  })
})
