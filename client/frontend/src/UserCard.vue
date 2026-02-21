<script setup lang="ts">
import { computed } from 'vue'
import type { User } from './types'
import { VolumeX, Volume2, X } from 'lucide-vue-next'

const props = defineProps<{
  user: User
  speaking: boolean
  muted: boolean
  canKick: boolean
}>()

const emit = defineEmits<{ toggleMute: [id: number]; kick: [id: number] }>()

const initials = computed(() => {
  const first = props.user.username.trim()[0]
  return first ? first.toUpperCase() : '?'
})
</script>

<template>
  <div
    class="flex flex-col items-center gap-2 select-none group"
    :aria-label="`${user.username}${speaking ? ', speaking' : ''}${muted ? ', muted' : ''}`"
    role="listitem"
  >
    <!-- Avatar -->
    <div class="relative">
      <div class="avatar avatar-placeholder">
        <div
          class="bg-primary text-primary-content w-16 rounded-full ring-3 ring-offset-2 ring-offset-base-100 transition-all duration-150"
          :class="[speaking && !muted ? 'ring-success' : 'ring-transparent', muted ? 'opacity-40' : '']"
        >
          <span class="text-lg font-bold">{{ initials }}</span>
        </div>
      </div>
      <!-- Muted indicator badge -->
      <div
        v-if="muted"
        class="badge badge-error absolute -bottom-1 -right-1 w-5 h-5 !p-0"
        aria-hidden="true"
      >
        <VolumeX class="w-3 h-3 text-error-content" aria-hidden="true" />
      </div>
    </div>

    <span class="text-sm font-medium max-w-24 truncate" :class="muted ? 'opacity-40' : ''">
      {{ user.username }}
    </span>

    <!-- Action buttons row — reveals on group-hover -->
    <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
      <!-- Mute toggle -->
      <button
        class="btn btn-ghost btn-xs"
        :class="muted ? 'text-error' : 'text-base-content/60'"
        :title="muted ? `Unmute ${user.username}` : `Mute ${user.username}`"
        :aria-pressed="muted"
        @click.stop="emit('toggleMute', user.id)"
      >
        <VolumeX v-if="muted" class="w-3.5 h-3.5" aria-hidden="true" />
        <Volume2 v-else class="w-3.5 h-3.5" aria-hidden="true" />
        {{ muted ? 'Unmute' : 'Mute' }}
      </button>

      <!-- Kick button — only visible to room owner, not on self -->
      <button
        v-if="canKick"
        class="btn btn-ghost btn-xs text-error"
        :title="`Kick ${user.username}`"
        @click.stop="emit('kick', user.id)"
      >
        <X class="w-3.5 h-3.5" aria-hidden="true" />
        Kick
      </button>
    </div>
  </div>
</template>
