import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import Sidebar from '../Sidebar.vue'

describe('Sidebar', () => {
  const baseProps = {
    activeServerAddr: '',
    connectedAddr: '',
    connected: false,
    voiceConnected: false,
    connectError: '',
    startupAddr: '',
    globalUsername: 'TestUser',
  }

  const mountOpts = {
    global: { stubs: { Teleport: true } },
  }

  it('mounts without errors', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('renders default Local Dev server icon', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    expect(w.text()).toContain('L')
  })

  it('emits selectServer when a server icon is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const serverBtn = w.findAll('button').find(b => b.attributes('aria-label')?.includes('Open'))
    if (serverBtn) {
      await serverBtn.trigger('click')
      expect(w.emitted('selectServer')).toBeDefined()
    }
  })

  it('shows active ring on connected server', async () => {
    const w = mount(Sidebar, {
      props: { ...baseProps, activeServerAddr: 'localhost:8080' },
      ...mountOpts,
    })
    await flushPromises()
    expect(w.html()).toContain('ring-2')
  })

  it('shows green connected indicator for connected server', async () => {
    const w = mount(Sidebar, {
      props: { ...baseProps, connectedAddr: 'localhost:8080' },
      ...mountOpts,
    })
    await flushPromises()
    expect(w.find('.bg-success').exists()).toBe(true)
  })

  it('renders user avatar at bottom with initials', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    expect(avatar.exists()).toBe(true)
    expect(avatar.text()).toBe('T')
  })

  it('shows user menu with Rename Username and User Settings when avatar clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menu = w.find('[data-testid="user-menu"]')
    expect(menu.exists()).toBe(true)
    expect(menu.text()).toContain('Rename Username')
    expect(menu.text()).toContain('User Settings')
  })

  it('emits openSettings when User Settings is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menuBtns = w.findAll('[data-testid="user-menu"] a')
    const settingsBtn = menuBtns.find(b => b.text().includes('User Settings'))
    expect(settingsBtn).toBeTruthy()
    await settingsBtn!.trigger('click')
    expect(w.emitted('openSettings')).toBeTruthy()
  })

  it('opens rename modal when Rename Username is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menuBtns = w.findAll('[data-testid="user-menu"] a')
    const renameBtn = menuBtns.find(b => b.text().includes('Rename Username'))
    expect(renameBtn).toBeTruthy()
    await renameBtn!.trigger('click')
    await nextTick()
    expect(w.text()).toContain('Set Username')
  })

  it('emits renameUsername when rename modal is confirmed', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menuBtns = w.findAll('[data-testid="user-menu"] a')
    const renameBtn = menuBtns.find(b => b.text().includes('Rename Username'))
    await renameBtn!.trigger('click')
    await nextTick()

    const input = w.find('.modal input[type="text"]')
    expect(input.exists()).toBe(true)
    await input.setValue('NewName')

    const saveBtn = w.findAll('.modal button').find(b => b.text().includes('Save'))
    expect(saveBtn).toBeTruthy()
    await saveBtn!.trigger('click')

    const emitted = w.emitted('renameUsername')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toBe('NewName')
  })

  it('does not emit renameUsername when name is unchanged', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menuBtns = w.findAll('[data-testid="user-menu"] a')
    const renameBtn = menuBtns.find(b => b.text().includes('Rename Username'))
    await renameBtn!.trigger('click')
    await nextTick()

    const input = w.find('.modal input[type="text"]')
    expect((input.element as HTMLInputElement).value).toBe('TestUser')

    const saveBtn = w.findAll('.modal button').find(b => b.text().includes('Save'))
    await saveBtn!.trigger('click')

    expect(w.emitted('renameUsername')).toBeFalsy()
  })

  it('closes rename modal when Cancel is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const avatar = w.find('button[title="TestUser"]')
    await avatar.trigger('click')
    await nextTick()
    const menuBtns = w.findAll('[data-testid="user-menu"] a')
    const renameBtn = menuBtns.find(b => b.text().includes('Rename Username'))
    await renameBtn!.trigger('click')
    await nextTick()

    expect(w.text()).toContain('Set Username')

    const cancelBtn = w.findAll('.modal button').find(b => b.text().includes('Cancel'))
    expect(cancelBtn).toBeTruthy()
    await cancelBtn!.trigger('click')
    await nextTick()

    const dialog = w.find('dialog.modal')
    expect(dialog.classes()).not.toContain('modal-open')
  })

  it('renders Home button', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const homeBtn = w.find('[aria-label="Home"]')
    expect(homeBtn.exists()).toBe(true)
  })

  it('emits goHome when Home button is clicked', async () => {
    const w = mount(Sidebar, { props: baseProps, ...mountOpts })
    await flushPromises()
    const homeBtn = w.find('[aria-label="Home"]')
    await homeBtn.trigger('click')
    expect(w.emitted('goHome')).toBeTruthy()
  })
})
