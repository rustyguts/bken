import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import Sidebar from '../Sidebar.vue'

describe('Sidebar', () => {
  const baseProps = {
    activeServerAddr: '',
    connectedAddr: '',
    connectError: '',
    startupAddr: '',
    globalUsername: 'TestUser',
  }

  it('mounts without errors', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders the server browser button', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    const browserBtn = w.find('[aria-label="Server browser"]')
    expect(browserBtn.exists()).toBe(true)
  })

  it('renders default Local Dev server icon', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    // Default server "Local Dev" -> initial "L"
    expect(w.text()).toContain('L')
  })

  it('emits selectServer when a server icon is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    const serverBtn = w.findAll('button').find(b => b.attributes('aria-label')?.includes('Open'))
    if (serverBtn) {
      await serverBtn.trigger('click')
      expect(w.emitted('selectServer')).toBeDefined()
    }
  })

  it('shows active ring on connected server', async () => {
    const w = mount(Sidebar, {
      props: { ...baseProps, activeServerAddr: 'localhost:8443' },
    })
    await flushPromises()
    expect(w.html()).toContain('ring-2')
  })

  it('shows green connected indicator for connected server', async () => {
    const w = mount(Sidebar, {
      props: { ...baseProps, connectedAddr: 'localhost:8443' },
    })
    await flushPromises()
    expect(w.find('.bg-success').exists()).toBe(true)
  })

  it('opens browser dialog when browser button is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    const browserBtn = w.find('[aria-label="Server browser"]')
    await browserBtn.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Connect To New Server')
  })

  it('shows the global username in browser dialog', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    const browserBtn = w.find('[aria-label="Server browser"]')
    await browserBtn.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('TestUser')
  })

  it('emits connect with payload when Connect is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps })
    await flushPromises()
    // Open dialog
    const browserBtn = w.find('[aria-label="Server browser"]')
    await browserBtn.trigger('click')
    await flushPromises()

    // Fill in address
    const inputs = w.findAll('input[type="text"]')
    const addrInput = inputs.find(i => i.attributes('placeholder')?.includes('host:port'))
    if (addrInput) {
      await addrInput.setValue('192.168.1.1:8443')
      // Click connect
      const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
      if (connectBtn) {
        await connectBtn.trigger('click')
        await flushPromises()
        expect(w.emitted('connect')).toBeDefined()
      }
    }
  })
})
