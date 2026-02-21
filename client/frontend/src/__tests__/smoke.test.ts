import { describe, it, expect } from 'vitest'

describe('Test infrastructure smoke test', () => {
  it('vitest runs and assertions work', () => {
    expect(1 + 1).toBe(2)
  })

  it('jsdom provides document', () => {
    expect(document).toBeDefined()
    expect(document.createElement('div')).toBeTruthy()
  })

  it('Go bridge mock is available on window', () => {
    expect((window as any).go.main.App).toBeDefined()
    expect((window as any).go.main.App.Connect).toBeDefined()
  })
})
