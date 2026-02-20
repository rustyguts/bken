<script setup lang="ts">
import { ref } from 'vue'
import { Disconnect } from '../wailsjs/go/main/App'
import Sidebar from './Sidebar.vue'
import EventLog from './EventLog.vue'
import type { LogEvent } from './EventLog.vue'
import MetricsBar from './MetricsBar.vue'
import RoomBrowser from './RoomBrowser.vue'
import AudioSettings from './AudioSettings.vue'

defineProps<{
  users: { id: number; username: string }[]
  speakingUsers: Set<number>
  logEvents: LogEvent[]
}>()

const emit = defineEmits<{ disconnect: [] }>()

const settingsOpen = ref(false)

async function handleDisconnect() {
  await Disconnect()
  emit('disconnect')
}
</script>

<template>
  <div class="flex h-full overflow-hidden">
    <Sidebar
      :settings-open="settingsOpen"
      @settings-toggle="settingsOpen = !settingsOpen"
      @disconnect="handleDisconnect"
    />

    <!-- Left panel: event log + metrics -->
    <div class="flex flex-col border-r border-base-content/10 min-h-0" style="width:220px;min-width:220px">
      <EventLog :events="logEvents" class="flex-1 min-h-0" />
      <MetricsBar />
    </div>

    <!-- Right panel: room or settings -->
    <div class="flex-1 min-w-0">
      <AudioSettings v-if="settingsOpen" />
      <RoomBrowser v-else :users="users" :speaking-users="speakingUsers" />
    </div>
  </div>
</template>
