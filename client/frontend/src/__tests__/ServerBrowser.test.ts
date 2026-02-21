import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ServerBrowser from '../ServerBrowser.vue'

describe('ServerBrowser', () => {
  it('mounts without errors', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders the bken heading', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.text()).toContain('bken')
  })

  it('renders Voice Communication subtitle', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.text()).toContain('Voice Communication')
  })

  it('renders username input', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const input = w.find('input[type="text"]')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toContain('Enter username')
  })

  it('pre-fills username from config', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const input = w.find('input[type="text"]')
    expect((input.element as HTMLInputElement).value).toBe('TestUser')
  })

  it('renders default server entry (Local Dev)', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.text()).toContain('Local Dev')
    expect(w.text()).toContain('localhost:8443')
  })

  it('renders Servers heading', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.text()).toContain('Servers')
  })

  it('renders Connect button for each server', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    expect(connectBtn).toBeDefined()
  })

  it('renders Remove button for each server', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const removeBtn = w.findAll('button').find(b => b.attributes('title') === 'Remove server')
    expect(removeBtn).toBeDefined()
  })

  it('shows error when trying to connect without username', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    // Clear username
    const input = w.find('input[type="text"]')
    await input.setValue('')
    // Click Connect
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    await connectBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Please enter a username')
  })

  it('emits connect with payload when connecting', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    await connectBtn!.trigger('click')
    await flushPromises()
    expect(w.emitted('connect')).toEqual([
      [{ username: 'TestUser', addr: 'localhost:8443' }],
    ])
  })

  it('shows Add server form when + button is clicked', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const addBtn = w.find('[aria-label="Add server"]')
    await addBtn.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Add Server')
  })

  it('validates add server form requires both fields', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const addBtn = w.find('[aria-label="Add server"]')
    await addBtn.trigger('click')
    await flushPromises()
    // Click Add without filling form
    const submitBtn = w.findAll('button').find(b => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Name and address are required')
  })

  it('adds a new server when form is filled', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const addBtn = w.find('[aria-label="Add server"]')
    await addBtn.trigger('click')
    await flushPromises()

    const inputs = w.findAll('input[type="text"]')
    // Inputs: [username, server name, server addr]
    const nameInput = inputs.find(i => i.attributes('placeholder') === 'Name')
    const addrInput = inputs.find(i => i.attributes('placeholder')?.includes('host:port'))
    await nameInput!.setValue('My Server')
    await addrInput!.setValue('192.168.1.1:4433')

    const submitBtn = w.findAll('button').find(b => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('My Server')
    expect(w.text()).toContain('192.168.1.1:4433')
  })

  it('strips bken:// prefix from address when adding', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const addBtn = w.find('[aria-label="Add server"]')
    await addBtn.trigger('click')
    await flushPromises()

    const inputs = w.findAll('input[type="text"]')
    const nameInput = inputs.find(i => i.attributes('placeholder') === 'Name')
    const addrInput = inputs.find(i => i.attributes('placeholder')?.includes('host:port'))
    await nameInput!.setValue('Invite')
    await addrInput!.setValue('bken://192.168.1.5:4433')

    const submitBtn = w.findAll('button').find(b => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('192.168.1.5:4433')
    // Should not show the bken:// prefix as the address
    expect(w.text()).not.toContain('bken://')
  })

  it('cancels add form', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const addBtn = w.find('[aria-label="Add server"]')
    await addBtn.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Add Server')

    const cancelBtn = w.findAll('button').find(b => b.text() === 'Cancel')
    await cancelBtn!.trigger('click')
    await flushPromises()
    // Add Server form should be gone
    expect(w.text()).not.toContain('Add Server')
  })

  it('removes a server when remove button is clicked', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    expect(w.text()).toContain('Local Dev')
    const removeBtn = w.findAll('button').find(b => b.attributes('title') === 'Remove server')
    await removeBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).not.toContain('Local Dev')
    expect(w.text()).toContain('No servers')  // "No servers — click + to add one"
  })

  it('disables inputs when connecting', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    await connectBtn!.trigger('click')
    await flushPromises()
    // After clicking connect, the connecting state should be true
    // Username input should be disabled
    const input = w.find('input[autocomplete="username"]')
    expect(input.attributes('disabled')).toBeDefined()
  })

  it('shows Connecting text on the server entry while connecting', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    await connectBtn!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Connecting')
  })

  it('connects on Enter key when only one server exists', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    const input = w.find('input[autocomplete="username"]')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(w.emitted('connect')).toBeDefined()
  })

  it('exposes setError method', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    ;(w.vm as any).setError('Connection refused')
    await flushPromises()
    expect(w.text()).toContain('Connection refused')
  })

  it('exposes setStartupAddr method', async () => {
    const w = mount(ServerBrowser)
    await flushPromises()
    await (w.vm as any).setStartupAddr('10.0.0.1:4433')
    await flushPromises()
    expect(w.text()).toContain('Invited Server')
    expect(w.text()).toContain('10.0.0.1:4433')
  })
})
