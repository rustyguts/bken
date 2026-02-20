<script setup lang="ts">
import { computed } from 'vue'
import type { Channel, User } from './types'

const props = defineProps<{
  channels: Channel[]
  users: User[]
  userChannels: Record<number, number>
  myId: number
  selectedChannelId: number
  serverName: string
  speakingUsers: Set<number>
}>()

const emit = defineEmits<{
  join: [channelID: number]
  select: [channelID: number]
}>()

const myChannelId = computed(() => props.userChannels[props.myId] ?? 0)
const rows = computed(() => [{ id: 0, name: 'Lobby' }, ...props.channels])
const hasMyChannelState = computed(() => Object.prototype.hasOwnProperty.call(props.userChannels, props.myId))
const hasMeInUserList = computed(() => props.users.some(u => u.id === props.myId))

function usersForChannel(channelId: number): User[] {
  const users = props.users.filter(u => (props.userChannels[u.id] ?? 0) === channelId)
  if (props.myId > 0 && hasMyChannelState.value && !hasMeInUserList.value && myChannelId.value === channelId) {
    return [...users, { id: props.myId, username: 'You' }]
  }
  return users
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '??'
  const a = parts[0][0] ?? ''
  const b = parts[1]?.[0] ?? parts[0][1] ?? ''
  return `${a}${b}`.toUpperCase()
}

function selectChannel(channelId: number): void {
  emit('select', channelId)
}

function joinVoice(channelId: number, event: MouseEvent): void {
  event.stopPropagation()
  emit('join', channelId)
}

function isConnectedToChannel(channelId: number): boolean {
  return props.myId > 0 && hasMyChannelState.value && myChannelId.value === channelId
}
</script>

<template>
  <section class="flex flex-col h-full min-h-0">
    <div class="px-3 py-2 border-b border-base-content/10">
      <h2 class="text-xs font-semibold uppercase tracking-widest opacity-50">{{ serverName || 'Server' }}</h2>
    </div>

    <div class="flex-1 min-h-0 overflow-y-auto p-2 space-y-2">
      <div
        v-for="channel in rows"
        :key="channel.id"
        class="group w-full text-left rounded-md px-2 py-2 border transition cursor-pointer"
        :class="[
          selectedChannelId === channel.id ? 'bg-primary/15 border-primary/40' : 'bg-base-200 border-base-content/10 hover:border-base-content/30',
          isConnectedToChannel(channel.id) ? 'ring-1 ring-primary/30' : '',
        ]"
        @click="selectChannel(channel.id)"
      >
        <div class="flex items-center gap-2">
          <span class="text-xs opacity-70">{{ isConnectedToChannel(channel.id) ? '●' : '○' }}</span>
          <span class="text-sm font-medium truncate">{{ channel.name }}</span>
          <span class="badge badge-ghost badge-xs ml-auto">{{ usersForChannel(channel.id).length }}</span>
        </div>

        <div class="mt-2 flex items-center flex-wrap gap-1">
          <span
            v-for="user in usersForChannel(channel.id).slice(0, 6)"
            :key="`${channel.id}-${user.id}`"
            class="w-5 h-5 rounded-full border text-[9px] font-mono flex items-center justify-center transition-all duration-150"
            :class="speakingUsers.has(user.id) ? 'bg-success/20 border-success ring-1 ring-success/50' : 'bg-base-300 border-base-content/20'"
            :title="user.username"
          >
            {{ initials(user.username) }}
          </span>
          <span
            v-if="usersForChannel(channel.id).length > 6"
            class="badge badge-ghost badge-xs"
            :title="`${usersForChannel(channel.id).length - 6} more users`"
          >
            +{{ usersForChannel(channel.id).length - 6 }}
          </span>
          <span v-if="usersForChannel(channel.id).length === 0" class="text-[10px] opacity-40 italic">No users</span>

          <!-- Join Voice button (hover reveal, hidden if already in this channel) -->
          <button
            v-if="!isConnectedToChannel(channel.id)"
            class="btn btn-xs btn-primary ml-auto opacity-0 group-hover:opacity-100 transition-opacity"
            title="Connect to voice"
            @click="joinVoice(channel.id, $event)"
          >
            Join Voice
          </button>
          <span v-else class="text-[10px] ml-auto text-success opacity-80">Voice Connected</span>
        </div>
      </div>
    </div>
  </section>
</template>
