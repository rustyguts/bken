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

const selectedQuality = ref<Record<number, string>>({})

const activeVideoUsers = computed(() => {
  return props.users.filter(u => {
    const vs = props.videoStates[u.id]
    return vs && vs.active
  })
})

const hasVideo = computed(() => activeVideoUsers.value.length > 0)

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
  <div v-if="hasVideo" class="max-h-[40vh] overflow-y-auto bg-base-300 border-b border-base-content/10">
    <!-- Spotlight mode: single user full-width -->
    <div v-if="spotlightId !== null && videoStates[spotlightId]?.active" class="p-2">
      <div
        class="relative bg-base-200 rounded-lg overflow-hidden aspect-video flex items-center justify-center cursor-pointer"
        @dblclick="handleDoubleClick(spotlightId)"
      >
        <div class="avatar avatar-placeholder">
          <div class="bg-neutral text-neutral-content w-16 rounded-full">
            <span class="text-2xl">{{ initials(users.find(u => u.id === spotlightId)?.username ?? '?') }}</span>
          </div>
        </div>
        <div class="absolute bottom-2 left-2 flex items-center gap-1">
          <span class="badge badge-sm">
            {{ users.find(u => u.id === spotlightId)?.username ?? 'Unknown' }}
          </span>
          <span v-if="isScreenShare(spotlightId)" class="badge badge-sm badge-primary">Screen</span>
          <span v-if="spotlightId === myId" class="badge badge-sm badge-info">You</span>
        </div>
        <div class="absolute top-2 right-2 flex items-center gap-1">
          <select
            v-if="hasLayers(spotlightId) && spotlightId !== myId"
            class="select select-xs bg-black/40 text-white border-none min-h-0 h-6"
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
        class="relative bg-base-200 rounded-lg overflow-hidden aspect-video flex items-center justify-center cursor-pointer hover:ring-2 hover:ring-primary/40 transition-shadow"
        @dblclick="handleDoubleClick(user.id)"
        @contextmenu.prevent="emit('spotlight', user.id)"
      >
        <div class="avatar avatar-placeholder">
          <div class="bg-neutral text-neutral-content w-12 rounded-full">
            <span class="text-xl">{{ initials(user.username) }}</span>
          </div>
        </div>
        <div class="absolute bottom-1 left-1 flex items-center gap-1">
          <span class="badge badge-xs">{{ user.username }}</span>
          <span v-if="isScreenShare(user.id)" class="badge badge-xs badge-primary">Screen</span>
          <span v-if="user.id === myId" class="badge badge-xs badge-info">You</span>
        </div>
        <select
          v-if="hasLayers(user.id) && user.id !== myId"
          class="absolute top-1 right-1 select select-xs bg-black/40 text-white border-none min-h-0 h-5 text-[10px] opacity-0 hover:opacity-100 transition-opacity"
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
