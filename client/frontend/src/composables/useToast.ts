import { ref } from 'vue'

export type ToastType = 'error' | 'warning' | 'info' | 'success'

export interface Toast {
  id: number
  message: string
  type: ToastType
}

let nextId = 1
const toasts = ref<Toast[]>([])
const timers = new Map<number, ReturnType<typeof setTimeout>>()

function addToast(message: string, type: ToastType = 'error', duration = 5000): void {
  const id = nextId++
  toasts.value = [...toasts.value, { id, message, type }]
  const timer = setTimeout(() => {
    dismissToast(id)
  }, duration)
  timers.set(id, timer)
}

function dismissToast(id: number): void {
  const timer = timers.get(id)
  if (timer) {
    clearTimeout(timer)
    timers.delete(id)
  }
  toasts.value = toasts.value.filter(t => t.id !== id)
}

function clearToasts(): void {
  timers.forEach(t => clearTimeout(t))
  timers.clear()
  toasts.value = []
}

export function useToast() {
  return { toasts, addToast, dismissToast, clearToasts }
}
