<script setup lang="ts">
import UserCard from './UserCard.vue'

defineProps<{
  users: { id: number; username: string }[]
  speakingUsers: Set<number>
}>()
</script>

<template>
  <div class="flex flex-col h-full">
    <div class="px-4 py-2 text-xs font-semibold uppercase tracking-wider opacity-40 border-b border-base-content/10 shrink-0">
      Server
    </div>
    <div v-if="users.length === 0" class="flex-1 flex items-center justify-center">
      <p class="text-sm opacity-25 italic">No one else is here</p>
    </div>
    <div v-else class="flex-1 flex flex-wrap content-start gap-8 p-8 overflow-y-auto">
      <UserCard
        v-for="user in users"
        :key="user.id"
        :user="user"
        :speaking="speakingUsers.has(user.id)"
      />
    </div>
  </div>
</template>
