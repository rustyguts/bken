<script setup lang="ts">
import { computed } from 'vue'
import type { User } from './types'

const props = defineProps<{
  user: User
  x: number
  y: number
  isOwner: boolean
  myId: number
  ownerUserId: number
  userChannels: Record<number, number>
  speakingUsers: Set<number>
}>()

const popupLeft = computed(() => Math.min(props.x, window.innerWidth - 280))
const popupTop = computed(() => Math.min(props.y, window.innerHeight - 300))

const emit = defineEmits<{
  close: []
  kick: [userId: number]
  moveUser: [userId: number, channelId: number]
}>()

const roleLabel = computed(() => {
  if (props.user.id === props.ownerUserId) return 'Owner'
  return 'User'
})

const roleBadgeClass = computed(() => {
  if (props.user.id === props.ownerUserId) return 'badge-warning'
  return 'badge-ghost'
})

const statusLabel = computed(() => {
  if (!(props.user.id in props.userChannels)) return 'Offline'
  if (props.speakingUsers.has(props.user.id)) return 'Speaking'
  return 'In voice'
})

const statusDotClass = computed(() => {
  if (!(props.user.id in props.userChannels)) return 'bg-base-content/30'
  if (props.speakingUsers.has(props.user.id)) return 'bg-success'
  return 'bg-primary'
})

const showOwnerActions = computed(() => {
  return props.isOwner && props.user.id !== props.myId
})

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
}

function handleKick(): void {
  emit('kick', props.user.id)
  emit('close')
}

function handleClickOutside(e: MouseEvent): void {
  e.stopPropagation()
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[100]" @click="handleClickOutside" @keydown.escape="emit('close')">
      <div
        class="fixed z-[101] w-64 rounded-lg border border-base-content/15 bg-base-200 shadow-xl"
        :style="{ left: popupLeft + 'px', top: popupTop + 'px' }"
        @click.stop
      >
        <!-- Header -->
        <div class="p-4 flex items-center gap-3 border-b border-base-content/10">
          <div
            class="w-10 h-10 rounded-full bg-base-300 border border-base-content/20 text-sm font-mono font-semibold flex items-center justify-center shrink-0"
          >
            {{ initials(user.username) }}
          </div>
          <div class="min-w-0">
            <p class="text-sm font-semibold truncate">{{ user.username }}</p>
            <div class="flex items-center gap-1.5 mt-0.5">
              <span class="badge badge-xs" :class="roleBadgeClass">{{ roleLabel }}</span>
            </div>
          </div>
        </div>

        <!-- Status -->
        <div class="px-4 py-3 space-y-2">
          <div class="flex items-center gap-2 text-xs">
            <span class="w-2 h-2 rounded-full shrink-0" :class="statusDotClass" />
            <span class="opacity-70">{{ statusLabel }}</span>
          </div>
          <div class="text-[10px] opacity-40 font-mono">
            ID: {{ user.id }}
          </div>
        </div>

        <!-- Owner actions -->
        <div v-if="showOwnerActions" class="border-t border-base-content/10 p-2 flex gap-1">
          <button
            class="btn btn-xs btn-error btn-ghost flex-1"
            @click="handleKick"
          >
            Kick
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
