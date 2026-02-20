<script setup lang="ts">
import { computed } from 'vue'
import type { User } from './types'

const props = defineProps<{
  user: User
  speaking: boolean
}>()

const initials = computed(() => props.user.username.slice(0, 2).toUpperCase())
</script>

<template>
  <div
    class="flex flex-col items-center gap-2 select-none"
    :aria-label="`${user.username}${speaking ? ', speaking' : ''}`"
    role="listitem"
  >
    <div
      class="w-16 h-16 rounded-full flex items-center justify-center text-lg font-bold bg-primary text-primary-content ring-3 ring-offset-2 ring-offset-base-100 transition-[ring-color] duration-150"
      :class="speaking ? 'ring-success' : 'ring-transparent'"
    >
      {{ initials }}
    </div>
    <span class="text-sm font-medium max-w-24 truncate">{{ user.username }}</span>
  </div>
</template>
