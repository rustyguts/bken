<script setup lang="ts">
import { ref } from 'vue'
import { Disconnect, SetMuted, SetDeafened } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import EventLog from './EventLog.vue'
import MetricsBar from './MetricsBar.vue'
import RoomBrowser from './RoomBrowser.vue'
import AudioSettings from './AudioSettings.vue'
import type { User, LogEvent } from './types'

defineProps<{
  users: User[]
  speakingUsers: Set<number>
  logEvents: LogEvent[]
}>()

const emit = defineEmits<{ disconnect: [] }>()

const settingsOpen = ref(false)
const muted = ref(false)
const deafened = ref(false)

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

    <!-- Left panel: event log + metrics -->
    <div class="flex flex-col border-r border-base-content/10 min-h-0 w-[220px] min-w-[220px]">
      <EventLog :events="logEvents" class="flex-1 min-h-0" />
      <MetricsBar />
    </div>

    <!-- Right panel: room or settings -->
    <div class="flex-1 min-w-0 relative">
      <Transition name="fade" mode="out-in">
        <AudioSettings v-if="settingsOpen" key="settings" class="absolute inset-0" />
        <RoomBrowser v-else key="room" :users="users" :speaking-users="speakingUsers" class="absolute inset-0" />
      </Transition>
    </div>
  </div>
</template>
