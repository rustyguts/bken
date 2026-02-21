<script setup lang="ts">
import { computed, ref } from 'vue'
import type { User, VideoState } from './types'
import { RequestVideoQuality } from './config'
import { X } from 'lucide-vue-next'

const props = defineProps<{
  users: User[]
  videoStates: Record<number, VideoState>
  myId: number
  spotlightId: number | null
}>()

const emit = defineEmits<{
  spotlight: [userId: number | null]
}>()

/** Tracks the selected quality per user (defaults to "high"). */
const selectedQuality = ref<Record<number, string>>({})

/** Users who currently have video or screen share active. */
const activeVideoUsers = computed(() => {
  return props.users.filter(u => {
    const vs = props.videoStates[u.id]
    return vs && vs.active
  })
})

/** Whether any user has an active video feed. */
const hasVideo = computed(() => activeVideoUsers.value.length > 0)

/** Grid column class based on number of video tiles. */
const gridClass = computed(() => {
  const n = activeVideoUsers.value.length
  if (n <= 1) return 'grid-cols-1'
  if (n <= 2) return 'grid-cols-2'
  if (n <= 4) return 'grid-cols-2'
  return 'grid-cols-3'
})

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
}

function isScreenShare(userId: number): boolean {
  const vs = props.videoStates[userId]
  return vs?.screenShare ?? false
}

function handleDoubleClick(userId: number): void {
  if (props.spotlightId === userId) {
    emit('spotlight', null)
  } else {
    emit('spotlight', userId)
  }
}

function hasLayers(userId: number): boolean {
  const vs = props.videoStates[userId]
  return (vs?.layers?.length ?? 0) > 0
}

function currentQuality(userId: number): string {
  return selectedQuality.value[userId] ?? 'high'
}

async function setQuality(userId: number, quality: string): Promise<void> {
  selectedQuality.value = { ...selectedQuality.value, [userId]: quality }
  await RequestVideoQuality(userId, quality)
}
</script>

<template>
  <div v-if="hasVideo" class="video-grid-container bg-base-300 border-b border-base-content/10">
    <!-- Spotlight mode: single user full-width -->
    <div v-if="spotlightId !== null && videoStates[spotlightId]?.active" class="p-2">
      <div
        class="video-tile relative bg-base-200 rounded-lg overflow-hidden aspect-video flex items-center justify-center cursor-pointer"
        @dblclick="handleDoubleClick(spotlightId)"
      >
        <div class="w-16 h-16 rounded-full bg-base-300 border-2 border-base-content/20 flex items-center justify-center text-2xl font-mono">
          {{ initials(users.find(u => u.id === spotlightId)?.username ?? '?') }}
        </div>
        <div class="absolute bottom-2 left-2 flex items-center gap-1">
          <span class="text-xs bg-black/60 text-white px-2 py-0.5 rounded">
            {{ users.find(u => u.id === spotlightId)?.username ?? 'Unknown' }}
          </span>
          <span v-if="isScreenShare(spotlightId)" class="text-xs bg-primary/80 text-primary-content px-2 py-0.5 rounded">
            Screen
          </span>
          <span v-if="spotlightId === myId" class="text-xs bg-info/80 text-info-content px-2 py-0.5 rounded">
            You
          </span>
        </div>
        <div class="absolute top-2 right-2 flex items-center gap-1">
          <!-- Simulcast quality selector -->
          <select
            v-if="hasLayers(spotlightId) && spotlightId !== myId"
            class="select select-xs bg-black/40 text-white border-none focus:outline-none min-h-0 h-6"
            :value="currentQuality(spotlightId)"
            @change="setQuality(spotlightId, ($event.target as HTMLSelectElement).value)"
            @click.stop
          >
            <option v-for="layer in videoStates[spotlightId]?.layers" :key="layer.quality" :value="layer.quality">
              {{ layer.quality }} ({{ layer.width }}x{{ layer.height }})
            </option>
          </select>
          <button
            class="btn btn-xs btn-ghost text-white bg-black/40"
            title="Exit spotlight"
            @click.stop="emit('spotlight', null)"
          >
            <X class="w-4 h-4" aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>

    <!-- Grid mode -->
    <div v-else class="grid gap-1 p-2" :class="gridClass">
      <div
        v-for="user in activeVideoUsers"
        :key="user.id"
        class="video-tile relative bg-base-200 rounded-lg overflow-hidden aspect-video flex items-center justify-center cursor-pointer hover:ring-2 hover:ring-primary/40 transition-shadow"
        @dblclick="handleDoubleClick(user.id)"
        @contextmenu.prevent="emit('spotlight', user.id)"
      >
        <!-- Placeholder avatar (actual video rendering requires native frame piping) -->
        <div class="w-12 h-12 rounded-full bg-base-300 border-2 border-base-content/20 flex items-center justify-center text-xl font-mono">
          {{ initials(user.username) }}
        </div>
        <div class="absolute bottom-1 left-1 flex items-center gap-1">
          <span class="text-xs bg-black/60 text-white px-1.5 py-0.5 rounded">
            {{ user.username }}
          </span>
          <span v-if="isScreenShare(user.id)" class="text-xs bg-primary/80 text-primary-content px-1.5 py-0.5 rounded">
            Screen
          </span>
          <span v-if="user.id === myId" class="text-xs bg-info/80 text-info-content px-1.5 py-0.5 rounded">
            You
          </span>
        </div>
        <!-- Simulcast quality selector (grid) -->
        <select
          v-if="hasLayers(user.id) && user.id !== myId"
          class="absolute top-1 right-1 select select-xs bg-black/40 text-white border-none focus:outline-none min-h-0 h-5 text-[10px] opacity-0 hover:opacity-100 transition-opacity"
          :value="currentQuality(user.id)"
          @change="setQuality(user.id, ($event.target as HTMLSelectElement).value)"
          @click.stop
          @dblclick.stop
        >
          <option v-for="layer in videoStates[user.id]?.layers" :key="layer.quality" :value="layer.quality">
            {{ layer.quality }}
          </option>
        </select>
      </div>
    </div>
  </div>
</template>

<style scoped>
.video-grid-container {
  max-height: 40vh;
  overflow-y: auto;
}
</style>
