<script setup lang="ts">
import { ref, nextTick, watch } from 'vue'
import type { ChatMessage } from './types'

const props = defineProps<{ messages: ChatMessage[] }>()
const emit = defineEmits<{ send: [message: string] }>()

const input = ref('')
const scrollEl = ref<HTMLElement | null>(null)

watch(
  () => props.messages.length,
  async () => {
    await nextTick()
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  },
)

function send(): void {
  const text = input.value.trim()
  if (!text) return
  emit('send', text)
  input.value = ''
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString('en', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <div class="flex flex-col h-full min-h-0">
    <!-- Messages area -->
    <div ref="scrollEl" class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <div v-if="messages.length === 0" class="text-base-content/40 text-sm text-center pt-6 select-none">
        No messages yet
      </div>
      <div v-for="msg in messages" :key="msg.id" class="flex flex-col gap-0.5">
        <div class="flex items-baseline gap-2">
          <span class="font-semibold text-sm text-primary truncate max-w-[140px]">{{ msg.username }}</span>
          <span class="text-xs text-base-content/40 shrink-0">{{ formatTime(msg.ts) }}</span>
        </div>
        <p class="text-sm text-base-content break-words leading-snug">{{ msg.message }}</p>
      </div>
    </div>

    <!-- Input area -->
    <div class="border-t border-base-content/10 p-2 flex gap-2 shrink-0">
      <input
        v-model="input"
        type="text"
        placeholder="Send a message…"
        maxlength="500"
        class="input input-sm input-bordered flex-1 min-w-0"
        @keydown.enter.prevent="send"
      />
      <button class="btn btn-sm btn-primary" :disabled="!input.trim()" @click="send">Send</button>
    </div>
  </div>
</template>
