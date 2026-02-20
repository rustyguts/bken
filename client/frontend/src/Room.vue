<script setup lang="ts">
import { ref } from 'vue'
import { Disconnect, SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import EventLog from './EventLog.vue'
import MetricsBar from './MetricsBar.vue'
import RoomBrowser from './RoomBrowser.vue'
import AudioSettings from './AudioSettings.vue'
import ChatPanel from './ChatPanel.vue'
import type { User, LogEvent, ChatMessage } from './types'

defineProps<{
  users: User[]
  speakingUsers: Set<number>
  logEvents: LogEvent[]
  chatMessages: ChatMessage[]
}>()

const emit = defineEmits<{ disconnect: []; sendChat: [message: string] }>()

const settingsOpen = ref(false)
const muted = ref(false)
const deafened = ref(false)
const activeTab = ref<'voice' | 'chat'>('voice')

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
            </button>
          </div>

          <!-- Tab content -->
          <div class="flex-1 min-h-0">
            <RoomBrowser
              v-if="activeTab === 'voice'"
              :users="users"
              :speaking-users="speakingUsers"
              class="h-full"
            />
            <ChatPanel
              v-else
              :messages="chatMessages"
              class="h-full"
              @send="emit('sendChat', $event)"
            />
          </div>
        </div>
      </Transition>
    </div>
  </div>
</template>
