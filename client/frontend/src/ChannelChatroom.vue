<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import type { ChatMessage, Channel } from './types'

const props = defineProps<{
  messages: ChatMessage[]
  channels: Channel[]
  selectedChannelId: number
  myChannelId: number
  connected: boolean
}>()

const emit = defineEmits<{
  selectChannel: [channelID: number]
  send: [message: string]
}>()

const input = ref('')
const scrollEl = ref<HTMLElement | null>(null)

const channelTabs = computed(() => [{ id: 0, name: 'Lobby' }, ...props.channels])

const selectedChannelName = computed(() => {
  const found = channelTabs.value.find(ch => ch.id === props.selectedChannelId)
  return found?.name ?? 'Lobby'
})

const visibleMessages = computed(() => {
  return props.messages.filter(msg => msg.channelId === props.selectedChannelId)
})

watch(
  () => [visibleMessages.value.length, props.selectedChannelId],
  async () => {
    await nextTick()
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  },
)

function send(): void {
  const text = input.value.trim()
  if (!text || !props.connected) return
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
  <section class="flex flex-col h-full min-h-0 bg-base-100">
    <header class="border-b border-base-content/10 px-3 py-2 flex flex-col gap-2">
      <div class="flex items-center justify-between">
        <h2 class="text-sm font-semibold"># {{ selectedChannelName }}</h2>
        <span class="text-[11px] opacity-50">Chatroom</span>
      </div>

      <div class="flex gap-1 overflow-x-auto pb-1">
        <button
          v-for="channel in channelTabs"
          :key="channel.id"
          class="btn btn-xs whitespace-nowrap"
          :class="channel.id === selectedChannelId ? 'btn-primary' : 'btn-ghost'"
          @click="emit('selectChannel', channel.id)"
        >
          {{ channel.name }}
        </button>
      </div>
    </header>

    <div ref="scrollEl" class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <div v-if="!connected" class="text-sm opacity-40 text-center pt-6">Connect to a server to start chatting</div>
      <div v-else-if="visibleMessages.length === 0" class="text-sm opacity-40 text-center pt-6">No messages in this channel yet</div>

      <article
        v-for="msg in visibleMessages"
        :key="msg.id"
        class="rounded-lg border border-base-content/10 bg-base-200 p-2"
      >
        <div class="flex items-center gap-2 mb-1">
          <span class="text-xs font-semibold text-primary">{{ msg.username }}</span>
          <span class="text-[11px] opacity-50">{{ formatTime(msg.ts) }}</span>
        </div>
        <p class="text-sm break-words">{{ msg.message }}</p>
      </article>
    </div>

    <footer class="border-t border-base-content/10 p-2">
      <input
        v-model="input"
        type="text"
        maxlength="500"
        class="input input-sm input-bordered w-full"
        :placeholder="connected ? `Message #${selectedChannelName}` : 'Disconnected'"
        :disabled="!connected"
        @keydown.enter.prevent="send"
      />
    </footer>
  </section>
</template>
