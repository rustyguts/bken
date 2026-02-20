import { ref } from 'vue'
import { Connect } from '../../wailsjs/go/main/App'

/** Exponential backoff delays in seconds. */
const BACKOFF = [1, 2, 4, 8, 16, 30] as const

const reconnecting = ref(false)
const reconnectAttempt = ref(0)
const reconnectSecondsLeft = ref(0)
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let lastAddr = ''
let lastUsername = ''

function clearTimers(): void {
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

function scheduleReconnect(onSuccess: () => void, onAttempt: (attempt: number) => void): void {
  const delay = BACKOFF[Math.min(reconnectAttempt.value, BACKOFF.length - 1)]
  reconnectSecondsLeft.value = delay

  countdownTimer = setInterval(() => {
    reconnectSecondsLeft.value = Math.max(0, reconnectSecondsLeft.value - 1)
  }, 1000)

  reconnectTimer = setTimeout(async () => {
    clearInterval(countdownTimer!)
    countdownTimer = null
    reconnectAttempt.value++
    onAttempt(reconnectAttempt.value)

    const err = await Connect(lastAddr, lastUsername)
    if (!err) {
      reconnecting.value = false
      reconnectAttempt.value = 0
      onSuccess()
    } else {
      scheduleReconnect(onSuccess, onAttempt)
    }
  }, delay * 1000)
}

function startReconnect(onSuccess: () => void, onAttempt: (attempt: number) => void): void {
  if (reconnecting.value) return
  reconnecting.value = true
  scheduleReconnect(onSuccess, onAttempt)
}

function cancelReconnect(): void {
  clearTimers()
  reconnecting.value = false
  reconnectAttempt.value = 0
}

function setLastCredentials(addr: string, username: string): void {
  lastAddr = addr
  lastUsername = username
}

export function useReconnect() {
  return {
    reconnecting,
    reconnectAttempt,
    reconnectSecondsLeft,
    startReconnect,
    cancelReconnect,
    clearTimers,
    setLastCredentials,
  }
}
