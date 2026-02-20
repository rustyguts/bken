<script setup lang="ts">
import { computed } from 'vue'
import type { User } from './types'

const props = defineProps<{
  user: User
  speaking: boolean
  muted: boolean
  canKick: boolean
}>()

const emit = defineEmits<{ toggleMute: [id: number]; kick: [id: number] }>()

const initials = computed(() => props.user.username.slice(0, 2).toUpperCase())
</script>

<template>
  <div
    class="flex flex-col items-center gap-2 select-none group"
    :aria-label="`${user.username}${speaking ? ', speaking' : ''}${muted ? ', muted' : ''}`"
    role="listitem"
  >
    <!-- Avatar -->
    <div class="relative">
      <div
        class="w-16 h-16 rounded-full flex items-center justify-center text-lg font-bold bg-primary text-primary-content ring-3 ring-offset-2 ring-offset-base-100 transition-all duration-150"
        :class="[speaking && !muted ? 'ring-success' : 'ring-transparent', muted ? 'opacity-40' : '']"
      >
        {{ initials }}
      </div>
      <!-- Muted badge -->
      <div
        v-if="muted"
        class="absolute -bottom-1 -right-1 w-5 h-5 rounded-full bg-error flex items-center justify-center"
        aria-hidden="true"
      >
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3 text-error-content">
          <path d="M9.547 3.062A.75.75 0 0 1 10 3.75v12.5a.75.75 0 0 1-1.264.546L4.703 13H3.167a.75.75 0 0 1-.7-.48A6.985 6.985 0 0 1 2 10c0-.887.165-1.737.468-2.52a.75.75 0 0 1 .699-.48H4.7l4.033-3.296a.75.75 0 0 1 .814-.142ZM13.78 7.22a.75.75 0 1 0-1.06 1.06L14.44 10l-1.72 1.72a.75.75 0 0 0 1.06 1.06L15.5 11.06l1.72 1.72a.75.75 0 1 0 1.06-1.06L16.56 10l1.72-1.72a.75.75 0 0 0-1.06-1.06L15.5 8.94l-1.72-1.72Z" />
        </svg>
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
        <!-- Speaker-off (currently muted) -->
        <svg v-if="muted" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
          <path d="M9.547 3.062A.75.75 0 0 1 10 3.75v12.5a.75.75 0 0 1-1.264.546L4.703 13H3.167a.75.75 0 0 1-.7-.48A6.985 6.985 0 0 1 2 10c0-.887.165-1.737.468-2.52a.75.75 0 0 1 .699-.48H4.7l4.033-3.296a.75.75 0 0 1 .814-.142ZM13.78 7.22a.75.75 0 1 0-1.06 1.06L14.44 10l-1.72 1.72a.75.75 0 0 0 1.06 1.06L15.5 11.06l1.72 1.72a.75.75 0 1 0 1.06-1.06L16.56 10l1.72-1.72a.75.75 0 0 0-1.06-1.06L15.5 8.94l-1.72-1.72Z" />
        </svg>
        <!-- Speaker (not muted) -->
        <svg v-else xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
          <path d="M9.547 3.062A.75.75 0 0 1 10 3.75v12.5a.75.75 0 0 1-1.264.546L4.703 13H3.167a.75.75 0 0 1-.7-.48A6.985 6.985 0 0 1 2 10c0-.887.165-1.737.468-2.52a.75.75 0 0 1 .699-.48H4.7l4.033-3.296a.75.75 0 0 1 .814-.142ZM13.5 8a.75.75 0 0 1 .75.75 3.5 3.5 0 0 1 0 4.5.75.75 0 0 1-1.06-1.06 2 2 0 0 0 0-2.38.75.75 0 0 1 .31-1.01Z" />
        </svg>
        {{ muted ? 'Unmute' : 'Mute' }}
      </button>

      <!-- Kick button — only visible to room owner, not on self -->
      <button
        v-if="canKick"
        class="btn btn-ghost btn-xs text-error"
        :title="`Kick ${user.username}`"
        @click.stop="emit('kick', user.id)"
      >
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
          <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
        </svg>
        Kick
      </button>
    </div>
  </div>
</template>
