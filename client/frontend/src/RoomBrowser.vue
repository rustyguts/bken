<script setup lang="ts">
import { ref, computed } from 'vue'
import UserCard from './UserCard.vue'
import { MuteUser, UnmuteUser, KickUser, JoinChannel } from './config'
import type { User, Channel } from './types'

const props = defineProps<{
  users: User[]
  speakingUsers: Set<number>
  ownerId: number
  myId: number
  channels: Channel[]
  userChannels: Record<number, number>
}>()

const mutedUsers = ref<Set<number>>(new Set())

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)

function usersInChannel(channelId: number): User[] {
  return props.users.filter(u => (props.userChannels[u.id] ?? 0) === channelId)
}

const canKick = computed(() => props.ownerId === props.myId && props.myId !== 0)

async function handleToggleMute(id: number): Promise<void> {
  if (mutedUsers.value.has(id)) {
    mutedUsers.value.delete(id)
    mutedUsers.value = new Set(mutedUsers.value)
    await UnmuteUser(id)
  } else {
    mutedUsers.value.add(id)
    mutedUsers.value = new Set(mutedUsers.value)
    await MuteUser(id)
  }
}

async function handleKick(id: number): Promise<void> {
  await KickUser(id)
}

async function handleJoinChannel(id: number): Promise<void> {
  await JoinChannel(id)
}
</script>

<template>
  <div class="flex flex-col h-full overflow-y-auto">

    <!-- Named channels -->
    <template v-if="channels.length > 0">
      <div v-for="ch in channels" :key="ch.id" class="px-3 pt-2 pb-1">
        <!-- Channel header: click to join -->
        <button
          class="flex items-center gap-1.5 w-full text-left px-1 py-0.5 rounded-md transition-opacity"
          :class="myChannelId === ch.id
            ? 'opacity-100 text-primary font-semibold'
            : 'opacity-50 hover:opacity-80 font-semibold'"
          @click="handleJoinChannel(ch.id)"
          :title="myChannelId === ch.id ? 'Your current channel' : `Join ${ch.name}`"
        >
          <span class="text-[10px]">{{ myChannelId === ch.id ? '●' : '○' }}</span>
          <span class="text-xs uppercase tracking-wider">{{ ch.name }}</span>
          <span class="text-xs font-normal opacity-50 ml-auto">
            {{ usersInChannel(ch.id).length }}
          </span>
        </button>

        <!-- Users in this channel -->
        <div v-if="usersInChannel(ch.id).length === 0" class="text-xs opacity-25 italic px-3 py-1">
          Empty — click to join
        </div>
        <div v-else class="flex flex-wrap justify-start gap-3 px-2 py-1" role="list">
          <UserCard
            v-for="user in usersInChannel(ch.id)"
            :key="user.id"
            :user="user"
            :speaking="speakingUsers.has(user.id)"
            :muted="mutedUsers.has(user.id)"
            :can-kick="canKick && user.id !== myId"
            @toggle-mute="handleToggleMute"
            @kick="handleKick"
          />
        </div>
      </div>

      <!-- Lobby: users not in any channel (channel_id = 0) -->
      <div v-if="usersInChannel(0).length > 0" class="px-3 pt-2 pb-1">
        <div class="flex items-center gap-1.5 px-1 py-0.5 opacity-35 mb-0.5">
          <span class="text-[10px]">○</span>
          <span class="text-xs uppercase tracking-wider font-semibold">Lobby</span>
          <span class="text-xs font-normal opacity-50 ml-auto">{{ usersInChannel(0).length }}</span>
        </div>
        <div class="flex flex-wrap justify-start gap-3 px-2 py-1" role="list">
          <UserCard
            v-for="user in usersInChannel(0)"
            :key="user.id"
            :user="user"
            :speaking="speakingUsers.has(user.id)"
            :muted="mutedUsers.has(user.id)"
            :can-kick="canKick && user.id !== myId"
            @toggle-mute="handleToggleMute"
            @kick="handleKick"
          />
        </div>
      </div>
    </template>

    <!-- Fallback: no channels configured yet (flat list) -->
    <template v-else>
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
          :can-kick="canKick && user.id !== myId"
          @toggle-mute="handleToggleMute"
          @kick="handleKick"
        />
      </div>
    </template>

  </div>
</template>
