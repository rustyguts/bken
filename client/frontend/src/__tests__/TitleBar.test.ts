import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TitleBar from '../TitleBar.vue'

describe('TitleBar', () => {
  it('mounts without errors', () => {
    const w = mount(TitleBar, { props: {} })
    expect(w.exists()).toBe(true)
  })

  it('renders the bken branding', () => {
    const w = mount(TitleBar)
    expect(w.text()).toContain('bken')
  })

  it('does not show server name when not provided', () => {
    const w = mount(TitleBar)
    // Only the "bken" branding should appear
    const text = w.text().replace(/\s+/g, ' ').trim()
    expect(text).toMatch(/^bken/)
  })

  it('renders server name when provided', () => {
    const w = mount(TitleBar, { props: { serverName: 'My Server' } })
    expect(w.text()).toContain('My Server')
  })

  it('shows rename button when isOwner=true and serverName exists', () => {
    const w = mount(TitleBar, {
      props: { serverName: 'Test', isOwner: true, serverAddr: '1.2.3.4:8443' },
    })
    const editBtn = w.findAll('button').find(b => b.attributes('title') === 'Rename server')
    expect(editBtn).toBeDefined()
  })

  it('does NOT show rename button when isOwner=false', () => {
    const w = mount(TitleBar, {
      props: { serverName: 'Test', isOwner: false },
    })
    const editBtn = w.findAll('button').find(b => b.attributes('title') === 'Rename server')
    expect(editBtn).toBeUndefined()
  })

  it('shows copy invite link button for owner with server addr', () => {
    const w = mount(TitleBar, {
      props: { serverName: 'Test', isOwner: true, serverAddr: '1.2.3.4:8443' },
    })
    const copyBtn = w.findAll('button').find(b => b.attributes('title')?.includes('invite'))
    expect(copyBtn).toBeDefined()
  })

  it('copies bken:// link to clipboard when copy button is clicked', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })

    const w = mount(TitleBar, {
      props: { serverName: 'Test', isOwner: true, serverAddr: '1.2.3.4:8443' },
    })
    const copyBtn = w.findAll('button').find(b => b.attributes('title')?.includes('invite'))
    await copyBtn!.trigger('click')
    expect(writeText).toHaveBeenCalledWith('bken://1.2.3.4:8443')
  })

  it('has window control buttons (minimise, maximise, close)', () => {
    const w = mount(TitleBar)
    const minimiseBtn = w.find('[aria-label="Minimise window"]')
    const maximiseBtn = w.find('[aria-label="Maximise window"]')
    const closeBtn = w.find('[aria-label="Close window"]')
    expect(minimiseBtn.exists()).toBe(true)
    expect(maximiseBtn.exists()).toBe(true)
    expect(closeBtn.exists()).toBe(true)
  })
})
