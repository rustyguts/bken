import { test, expect } from '@playwright/test'

// Server address as seen from the browser inside Docker compose network.
const SERVER_ADDR = process.env.SERVER_ADDR || 'server:8080'

/**
 * Helper: connect to the bken server from the WelcomePage.
 * Fills in the "Connect to New Server" form and waits for the connected state.
 */
async function connectToServer(page: import('@playwright/test').Page, username?: string) {
  // Wait for WelcomePage to render
  await expect(page.locator('h1:has-text("bken")')).toBeVisible({ timeout: 15_000 })

  // Set a custom username via localStorage before connecting so both
  // browser tabs get distinct names.
  if (username) {
    await page.evaluate((name) => {
      const raw = localStorage.getItem('bken_config')
      const cfg = raw ? JSON.parse(raw) : {}
      cfg.username = name
      localStorage.setItem('bken_config', JSON.stringify(cfg))
    }, username)
    // Reload so the app picks up the stored username
    await page.reload()
    await expect(page.locator('h1:has-text("bken")')).toBeVisible({ timeout: 15_000 })
  }

  // Fill in the server address and connect
  const addrInput = page.locator('input[placeholder="host:port or bken:// link"]')
  await addrInput.fill(SERVER_ADDR)
  await page.locator('button:has-text("Connect")').click()

  // Wait for connected state — the server dropdown header becomes visible
  await expect(
    page.locator('.dropdown [role="button"]'),
  ).toBeVisible({ timeout: 15_000 })
}

/**
 * Helper: create a channel via the browser transport bridge.
 * Returns a locator for the new channel's <li> in the sidebar.
 */
async function createChannel(page: import('@playwright/test').Page, name: string) {
  await page.evaluate((chName) => {
    ;(window as any).go.main.App.CreateChannel(chName)
  }, name)

  // Wait for the channel to appear in the sidebar menu.
  // Use filter() to get a single matching element even if there are other channels.
  const channelRow = page.locator('ul.menu > li').filter({ hasText: name })
  await expect(channelRow).toBeVisible({ timeout: 10_000 })
  return channelRow
}

test.describe('Voice Channel Join/Leave', () => {
  test('single user can join and leave a voice channel', async ({ page }) => {
    // Use a unique channel name to avoid collisions from server state persisting across retries
    const channelName = `voice-${Date.now()}`

    await page.goto('/')
    await connectToServer(page, 'Alice')

    const channelRow = await createChannel(page, channelName)

    // Hover to reveal the Join button, then click it
    await channelRow.hover()
    await channelRow.locator('button:has-text("Join")').click()

    // Assert: user avatar visible inside the channel's nested user list
    await expect(channelRow.locator('.avatar')).toBeVisible({ timeout: 5_000 })

    // Assert: "Leave Voice" button is visible
    const leaveBtn = page.locator('button:has-text("Leave Voice")')
    await expect(leaveBtn).toBeVisible()

    // Click "Leave Voice"
    await leaveBtn.click()

    // Assert: avatar disappears from the channel
    await expect(channelRow.locator('.avatar')).not.toBeVisible({ timeout: 5_000 })

    // Assert: "Leave Voice" button is gone
    await expect(leaveBtn).not.toBeVisible()
  })

  test('two users see each other join and leave', async ({ browser }) => {
    const channelName = `voice-${Date.now()}`

    const ctx1 = await browser.newContext()
    const ctx2 = await browser.newContext()
    const page1 = await ctx1.newPage()
    const page2 = await ctx2.newPage()

    // --- Page 1: connect and create a channel ---
    await page1.goto('/')
    await connectToServer(page1, 'User1')
    const ch1 = await createChannel(page1, channelName)

    // Page 1: join voice
    await ch1.hover()
    await ch1.locator('button:has-text("Join")').click()
    await expect(page1.locator('button:has-text("Leave Voice")')).toBeVisible({
      timeout: 5_000,
    })

    // --- Page 2: connect to the same server ---
    await page2.goto('/')
    await connectToServer(page2, 'User2')

    // Page 2 should see the channel with Page 1's avatar
    const ch2 = page2.locator('ul.menu > li').filter({ hasText: channelName })
    await expect(ch2).toBeVisible({ timeout: 10_000 })
    await expect(ch2.locator('.avatar')).toBeVisible({ timeout: 10_000 })

    // --- Page 1: leave voice ---
    await page1.locator('button:has-text("Leave Voice")').click()

    // Page 2: avatar should disappear
    await expect(ch2.locator('.avatar')).not.toBeVisible({ timeout: 10_000 })

    await ctx1.close()
    await ctx2.close()
  })
})
