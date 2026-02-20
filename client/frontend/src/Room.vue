<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Disconnect, SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import EventLog from './EventLog.vue'
import MetricsBar from './MetricsBar.vue'
import RoomBrowser from './RoomBrowser.vue'
import AudioSettings from './AudioSettings.vue'
import ChatPanel from './ChatPanel.vue'
import type { User, LogEvent, ChatMessage, Channel } from './types'

const props = defineProps<{
  users: User[]
  speakingUsers: Set<number>
  logEvents: LogEvent[]
  chatMessages: ChatMessage[]
  ownerId: number
  myId: number
  channels: Channel[]
  userChannels: Record<number, number>
}>()

const emit = defineEmits<{
  disconnect: []
  sendChat: [message: string]
  sendChannelChat: [channelID: number, message: string]
}>()

const settingsOpen = ref(false)
const muted = ref(false)
const deafened = ref(false)
const activeTab = ref<'voice' | 'chat' | 'channel'>('voice')
const unreadChat = ref(0)
const unreadChannel = ref(0)

/** The channel the current user is in (0 = lobby). */
const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)

/** Messages scoped to the server (channelId === 0). */
const serverMessages = computed(() => props.chatMessages.filter(m => m.channelId === 0))

/** Messages scoped to the user's current channel. */
const channelMessages = computed(() => props.chatMessages.filter(m => m.channelId === myChannelId.value && myChannelId.value !== 0))

watch(
  () => props.chatMessages.length,
  () => {
    const last = props.chatMessages[props.chatMessages.length - 1]
    if (!last) return
    if (last.channelId === 0 && activeTab.value !== 'chat') unreadChat.value++
    if (last.channelId !== 0 && last.channelId === myChannelId.value && activeTab.value !== 'channel') unreadChannel.value++
  },
)
watch(activeTab, (tab) => {
  if (tab === 'chat') unreadChat.value = 0
  if (tab === 'channel') unreadChannel.value = 0
})
// When leaving a channel, switch away from channel tab.
watch(myChannelId, (id) => {
  if (id === 0 && activeTab.value === 'channel') activeTab.value = 'voice'
  unreadChannel.value = 0
})

async function handleMuteToggle(): Promise<void> {
  muted.value = !muted.value
  await SetMuted(muted.value)
}

async function handleDeafenToggle(): Promise<void> {
  deafened.value = !deafened.value
  await SetDeafened(deafened.value)
}

async function handleDisconnect(): Promise<void> {
  await Disconnect()
  emit('disconnect')
}
</script>

<template>
  <div class="flex h-full overflow-hidden">
    <Sidebar
      :settings-open="settingsOpen"
      :muted="muted"
      :deafened="deafened"
      @settings-toggle="settingsOpen = !settingsOpen"
      @mute-toggle="handleMuteToggle"
      @deafen-toggle="handleDeafenToggle"
      @server-browser="handleDisconnect"
      @disconnect="handleDisconnect"
    />

    <!-- Left panel: event log + metrics.
         Hidden below md (768 px) so narrow windows give the main panel full width. -->
    <div class="hidden md:flex flex-col border-r border-base-content/10 min-h-0 w-[220px] min-w-[220px]">
      <EventLog :events="logEvents" class="flex-1 min-h-0" />
      <MetricsBar />
    </div>

    <!-- Right panel: settings overlay or tabbed voice/chat -->
    <div class="flex-1 min-w-0 relative">
      <Transition name="fade" mode="out-in">
        <AudioSettings v-if="settingsOpen" key="settings" class="absolute inset-0" />
        <div v-else key="main" class="absolute inset-0 flex flex-col">
          <!-- Tab bar -->
          <div role="tablist" class="tabs tabs-bordered shrink-0 px-2 pt-1">
            <button
              role="tab"
              class="tab"
              :class="{ 'tab-active': activeTab === 'voice' }"
              @click="activeTab = 'voice'"
            >
              Voice
            </button>
            <button
              role="tab"
              class="tab"
              :class="{ 'tab-active': activeTab === 'chat' }"
              @click="activeTab = 'chat'"
            >
              Chat
              <span v-if="unreadChat > 0" class="badge badge-xs badge-primary ml-1">{{ unreadChat }}</span>
            </button>
            <button
              v-if="myChannelId !== 0"
              role="tab"
              class="tab"
              :class="{ 'tab-active': activeTab === 'channel' }"
              @click="activeTab = 'channel'"
            >
              Channel
              <span v-if="unreadChannel > 0" class="badge badge-xs badge-secondary ml-1">{{ unreadChannel }}</span>
            </button>
          </div>

          <!-- Tab content -->
          <div class="flex-1 min-h-0">
            <RoomBrowser
              v-if="activeTab === 'voice'"
              :users="users"
              :speaking-users="speakingUsers"
              :owner-id="props.ownerId"
              :my-id="props.myId"
              :channels="props.channels"
              :user-channels="props.userChannels"
              class="h-full"
            />
            <ChatPanel
              v-else-if="activeTab === 'chat'"
              :messages="serverMessages"
              class="h-full"
              @send="emit('sendChat', $event)"
            />
            <ChatPanel
              v-else-if="activeTab === 'channel'"
              :messages="channelMessages"
              class="h-full"
              @send="emit('sendChannelChat', myChannelId, $event)"
            />
          </div>
        </div>
      </Transition>
    </div>
  </div>
</template>
