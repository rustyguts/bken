import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import EventLog from '../EventLog.vue'
import type { LogEvent } from '../types'

describe('EventLog', () => {
  it('mounts without errors', () => {
    const w = mount(EventLog, { props: { events: [] } })
    expect(w.exists()).toBe(true)
  })

  it('shows empty state when no events', () => {
    const w = mount(EventLog, { props: { events: [] } })
    expect(w.text()).toContain('No events yet')
  })

  it('renders events', () => {
    const events: LogEvent[] = [
      { id: 1, time: '12:30', type: 'join', text: 'Alice joined' },
      { id: 2, time: '12:31', type: 'leave', text: 'Bob left' },
    ]
    const w = mount(EventLog, { props: { events } })
    expect(w.text()).toContain('Alice joined')
    expect(w.text()).toContain('Bob left')
  })

  it('renders timestamps', () => {
    const events: LogEvent[] = [
      { id: 1, time: '14:22', type: 'info', text: 'Something happened' },
    ]
    const w = mount(EventLog, { props: { events } })
    expect(w.text()).toContain('14:22')
  })

  it('applies text-success class for join events', () => {
    const events: LogEvent[] = [
      { id: 1, time: '12:00', type: 'join', text: 'Joined' },
    ]
    const w = mount(EventLog, { props: { events } })
    expect(w.find('.text-success').exists()).toBe(true)
  })

  it('applies text-error class for leave events', () => {
    const events: LogEvent[] = [
      { id: 1, time: '12:00', type: 'leave', text: 'Left' },
    ]
    const w = mount(EventLog, { props: { events } })
    expect(w.find('.text-error').exists()).toBe(true)
  })

  it('has role=log and aria-label', () => {
    const w = mount(EventLog, { props: { events: [] } })
    const logEl = w.find('[role="log"]')
    expect(logEl.exists()).toBe(true)
    expect(logEl.attributes('aria-label')).toBe('Event log')
  })
})
