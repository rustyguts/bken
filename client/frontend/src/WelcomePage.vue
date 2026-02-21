<script setup lang="ts">
import { ref } from 'vue'
import type { ServerEntry } from './config'
import type { ConnectPayload } from './types'
import { BKEN_SCHEME } from './constants'
import { Server } from 'lucide-vue-next'

const props = defineProps<{
  servers: ServerEntry[]
  globalUsername: string
}>()

const emit = defineEmits<{
  connect: [payload: ConnectPayload]
}>()

const quickAddr = ref('')
const error = ref('')

function normalizeAddr(raw: string): string {
  let addr = raw.trim()
  if (addr.startsWith(BKEN_SCHEME)) addr = addr.slice(BKEN_SCHEME.length)
  return addr
}

function connectToServer(addr: string): void {
  const user = props.globalUsername.trim()
  if (!user) {
    error.value = 'Set your username in settings first.'
    return
  }
  const normalized = normalizeAddr(addr)
  if (!normalized) {
    error.value = 'Server address is required.'
    return
  }
  error.value = ''
  emit('connect', { username: user, addr: normalized })
}

function handleQuickConnect(): void {
  connectToServer(quickAddr.value)
}
</script>

<template>
  <div class="flex items-center justify-center h-full bg-base-100 p-8">
    <div class="max-w-md w-full space-y-6">
      <div class="text-center space-y-2">
        <div class="flex items-center justify-center">
          <div class="w-16 h-16 rounded-2xl bg-primary/10 flex items-center justify-center">
            <Server class="w-8 h-8 text-primary" aria-hidden="true" />
          </div>
        </div>
        <h1 class="text-2xl font-bold">bken</h1>
        <p class="text-sm opacity-60">Select a server from the sidebar or connect to a new one below.</p>
      </div>

      <div v-if="servers.length > 0" class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wider opacity-50">Saved Servers</p>
        <div class="space-y-1">
          <button
            v-for="server in servers"
            :key="server.addr"
            class="btn btn-ghost btn-sm w-full justify-start gap-3 normal-case"
            @click="connectToServer(server.addr)"
          >
            <span class="w-6 h-6 rounded-full bg-base-300 border border-base-content/20 text-[9px] font-mono flex items-center justify-center shrink-0">
              {{ (server.name.trim()[0] || '?').toUpperCase() }}
            </span>
            <span class="flex-1 text-left truncate">{{ server.name }}</span>
            <span class="text-[10px] font-mono opacity-40">{{ server.addr }}</span>
          </button>
        </div>
      </div>

      <div class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wider opacity-50">Quick Connect</p>
        <div class="flex gap-2">
          <input
            v-model="quickAddr"
            type="text"
            placeholder="host:port"
            class="input input-sm input-bordered flex-1 font-mono"
            @keydown.enter.prevent="handleQuickConnect"
          />
          <button class="btn btn-primary btn-sm" @click="handleQuickConnect">Connect</button>
        </div>
      </div>

      <div v-if="error" class="alert alert-error py-2 text-sm">{{ error }}</div>
    </div>
  </div>
</template>
