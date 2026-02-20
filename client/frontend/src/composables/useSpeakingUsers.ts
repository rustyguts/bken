import { ref } from 'vue'

const speakingUsers = ref<Set<number>>(new Set())
const speakingTimers = new Map<number, ReturnType<typeof setTimeout>>()

function setSpeaking(id: number): void {
  const next = new Set(speakingUsers.value)
  next.add(id)
  speakingUsers.value = next
  const existing = speakingTimers.get(id)
  if (existing) clearTimeout(existing)
  speakingTimers.set(id, setTimeout(() => {
    const updated = new Set(speakingUsers.value)
    updated.delete(id)
    speakingUsers.value = updated
    speakingTimers.delete(id)
  }, 500))
}

function clearSpeaking(): void {
  speakingTimers.forEach(t => clearTimeout(t))
  speakingTimers.clear()
  speakingUsers.value = new Set()
}

function cleanup(): void {
  speakingTimers.forEach(t => clearTimeout(t))
}

export function useSpeakingUsers() {
  return { speakingUsers, setSpeaking, clearSpeaking, cleanup }
}
