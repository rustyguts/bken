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

const statusBadge = computed(() => {
  if (!(props.user.id in props.userChannels)) return 'badge-ghost'
  if (props.speakingUsers.has(props.user.id)) return 'badge-success'
  return 'badge-primary'
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
        class="card card-compact bg-base-200 shadow-xl border border-base-content/10 fixed z-[101] w-64"
        :style="{ left: popupLeft + 'px', top: popupTop + 'px' }"
        @click.stop
      >
        <div class="card-body">
          <!-- Header -->
          <div class="flex items-center gap-3">
            <div class="avatar avatar-placeholder">
              <div class="bg-neutral text-neutral-content w-10 rounded-full">
                <span class="text-sm">{{ initials(user.username) }}</span>
              </div>
            </div>
            <div class="min-w-0">
              <p class="card-title text-sm">{{ user.username }}</p>
              <span class="badge badge-xs" :class="roleBadgeClass">{{ roleLabel }}</span>
            </div>
          </div>

          <div class="divider my-0"></div>

          <!-- Status -->
          <div class="flex items-center gap-2 text-xs">
            <span class="badge badge-xs" :class="statusBadge" />
            <span class="opacity-70">{{ statusLabel }}</span>
          </div>
          <div class="text-[10px] opacity-40 font-mono">
            ID: {{ user.id }}
          </div>

          <!-- Owner actions -->
          <div v-if="showOwnerActions" class="card-actions justify-end mt-2">
            <button
              class="btn btn-xs btn-error btn-ghost"
              @click="handleKick"
            >
              Kick
            </button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
