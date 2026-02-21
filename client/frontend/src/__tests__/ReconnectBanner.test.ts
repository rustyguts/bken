import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ReconnectBanner from '../ReconnectBanner.vue'

describe('ReconnectBanner', () => {
  const defaultProps = {
    attempt: 1,
    secondsUntilRetry: 5,
    reason: 'Connection lost',
  }

  it('mounts without errors', () => {
    const w = mount(ReconnectBanner, { props: defaultProps })
    expect(w.exists()).toBe(true)
  })

  it('renders the reason text', () => {
    const w = mount(ReconnectBanner, { props: { ...defaultProps, reason: 'Server went away' } })
    expect(w.text()).toContain('Server went away')
  })

  it('shows retry countdown when secondsUntilRetry > 0', () => {
    const w = mount(ReconnectBanner, { props: { ...defaultProps, secondsUntilRetry: 3 } })
    expect(w.text()).toContain('retrying in 3s')
  })

  it('shows reconnecting text when secondsUntilRetry is 0', () => {
    const w = mount(ReconnectBanner, { props: { ...defaultProps, secondsUntilRetry: 0, attempt: 2 } })
    expect(w.text()).toContain('reconnecting')
    expect(w.text()).toContain('attempt 2')
  })

  it('shows default reason when empty', () => {
    const w = mount(ReconnectBanner, { props: { ...defaultProps, reason: '' } })
    expect(w.text()).toContain('Connection lost')
  })

  it('emits cancel when cancel button is clicked', async () => {
    const w = mount(ReconnectBanner, { props: defaultProps })
    await w.find('button').trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
  })

  it('has role=alert and aria-live=assertive', () => {
    const w = mount(ReconnectBanner, { props: defaultProps })
    const el = w.find('[role="alert"]')
    expect(el.exists()).toBe(true)
    expect(el.attributes('aria-live')).toBe('assertive')
  })
})
