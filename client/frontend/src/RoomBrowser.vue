<script setup lang="ts">
import { ref } from 'vue'
import UserCard from './UserCard.vue'
import { MuteUser, UnmuteUser } from './config'
import type { User } from './types'

defineProps<{
  users: User[]
  speakingUsers: Set<number>
}>()

const mutedUsers = ref<Set<number>>(new Set())

async function handleToggleMute(id: number): Promise<void> {
  if (mutedUsers.value.has(id)) {
    mutedUsers.value.delete(id)
    // Trigger Vue reactivity by replacing the set.
    mutedUsers.value = new Set(mutedUsers.value)
    await UnmuteUser(id)
  } else {
    mutedUsers.value.add(id)
    mutedUsers.value = new Set(mutedUsers.value)
    await MuteUser(id)
  }
}
</script>

<template>
  <div class="flex flex-col h-full">
    <div class="px-4 py-2 text-xs font-semibold uppercase tracking-wider opacity-40 border-b border-base-content/10 shrink-0">
      Server
    </div>
    <div v-if="users.length === 0" class="flex-1 flex items-center justify-center">
      <p class="text-sm opacity-25 italic">No one else is here</p>
    </div>
    <div v-else class="flex-1 flex flex-wrap justify-center content-start gap-4 p-4 sm:gap-8 sm:p-8 overflow-y-auto" role="list" aria-label="Connected users">
      <UserCard
        v-for="user in users"
        :key="user.id"
        :user="user"
        :speaking="speakingUsers.has(user.id)"
        :muted="mutedUsers.has(user.id)"
        @toggle-mute="handleToggleMute"
      />
    </div>
  </div>
</template>
