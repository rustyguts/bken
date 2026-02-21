/**
 * Lightweight frontend logger gated on Vite's dev mode.
 * In production builds all calls are no-ops (tree-shaken by Vite).
 *
 * Usage:
 *   import { log } from './logger'
 *   log.debug('app', 'user joined', { id: 42 })
 *   log.info('chat', 'message sent', { channel: 'general' })
 */

const isDev = import.meta.env.DEV

function fmt(tag: string, msg: string): string {
  return `[bken:${tag}] ${msg}`
}

export const log = {
  debug(tag: string, msg: string, data?: unknown) {
    if (isDev) console.debug(fmt(tag, msg), data !== undefined ? data : '')
  },
  info(tag: string, msg: string, data?: unknown) {
    if (isDev) console.info(fmt(tag, msg), data !== undefined ? data : '')
  },
  warn(tag: string, msg: string, data?: unknown) {
    if (isDev) console.warn(fmt(tag, msg), data !== undefined ? data : '')
  },
  error(tag: string, msg: string, data?: unknown) {
    // Always log errors, even in production.
    console.error(fmt(tag, msg), data !== undefined ? data : '')
  },
}
