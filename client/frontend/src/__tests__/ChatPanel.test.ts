import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ChatPanel from '../ChatPanel.vue'
import type { ChatMessage } from '../types'

describe('ChatPanel', () => {
  it('mounts without errors', () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    expect(w.exists()).toBe(true)
  })

  it('shows empty state when no messages', () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    expect(w.text()).toContain('No messages yet')
  })

  it('renders messages with username and text', () => {
    const messages: ChatMessage[] = [
      { id: 1, msgId: 1, senderId: 1, username: 'Alice', message: 'Hello!', ts: Date.now(), channelId: 0 },
    ]
    const w = mount(ChatPanel, { props: { messages } })
    expect(w.text()).toContain('Alice')
    expect(w.text()).toContain('Hello!')
  })

  it('formats timestamps in HH:MM format', () => {
    const ts = new Date(2024, 0, 15, 14, 30).getTime()
    const messages: ChatMessage[] = [
      { id: 1, msgId: 1, senderId: 1, username: 'Alice', message: 'test', ts, channelId: 0 },
    ]
    const w = mount(ChatPanel, { props: { messages } })
    expect(w.text()).toContain('14:30')
  })

  it('renders multiple messages', () => {
    const messages: ChatMessage[] = [
      { id: 1, msgId: 1, senderId: 1, username: 'Alice', message: 'First', ts: 1000, channelId: 0 },
      { id: 2, msgId: 2, senderId: 2, username: 'Bob', message: 'Second', ts: 2000, channelId: 0 },
    ]
    const w = mount(ChatPanel, { props: { messages } })
    expect(w.text()).toContain('First')
    expect(w.text()).toContain('Second')
  })

  it('emits send on Enter key press', async () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    const input = w.find('input[type="text"]')
    await input.setValue('Hello world')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('send')).toEqual([['Hello world']])
  })

  it('does not emit send for empty input', async () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    const input = w.find('input[type="text"]')
    await input.setValue('  ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('send')).toBeUndefined()
  })

  it('clears input after sending', async () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    const input = w.find('input[type="text"]')
    await input.setValue('Hi')
    await input.trigger('keydown', { key: 'Enter' })
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('has maxlength=500 on input', () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    const input = w.find('input[type="text"]')
    expect(input.attributes('maxlength')).toBe('500')
  })

  it('has Send a message placeholder', () => {
    const w = mount(ChatPanel, { props: { messages: [] } })
    const input = w.find('input[type="text"]')
    expect(input.attributes('placeholder')).toContain('Send a message')
  })
})
