<script setup lang="ts">
import { ref, onMounted } from 'vue'
import type { ConnectPayload } from './types'
import { GetConfig, SaveConfig } from './config'

const emit = defineEmits<{
  connect: [payload: ConnectPayload]
}>()

const username = ref('')
const error = ref('')
const connecting = ref(false)
const connectingAddr = ref('')
const servers = ref([{ name: 'Local Dev', addr: 'localhost:4433' }])

onMounted(async () => {
  const cfg = await GetConfig()
  if (cfg.username) username.value = cfg.username
  if (cfg.servers?.length) servers.value = cfg.servers
})

async function handleConnect(addr: string): Promise<void> {
  if (!username.value.trim()) {
    error.value = 'Please enter a username'
    return
  }
  connecting.value = true
  connectingAddr.value = addr
  error.value = ''

  // Persist the username so it's pre-filled next time
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, username: username.value.trim() })

  emit('connect', { username: username.value.trim(), addr })
}

function setError(msg: string): void {
  error.value = msg
  connecting.value = false
  connectingAddr.value = ''
}

defineExpose({ setError })
</script>

<template>
  <div class="flex flex-col items-center justify-center h-full px-4 py-8 sm:p-8 gap-6 sm:gap-8">
    <div class="text-center">
      <h1 class="text-4xl font-bold tracking-widest">bken</h1>
      <p class="opacity-60 mt-1 text-sm">Voice Communication</p>
    </div>

    <!-- Username -->
    <div class="w-full max-w-xs">
      <label class="form-control w-full">
        <div class="label"><span class="label-text">Username</span></div>
        <input
          type="text"
          v-model="username"
          placeholder="Enter username"
          class="input input-bordered w-full"
          autocomplete="username"
          :disabled="connecting"
          @keydown.enter="servers.length === 1 && handleConnect(servers[0].addr)"
        />
      </label>
    </div>

    <!-- Server list -->
    <div class="w-full max-w-xs flex flex-col gap-2">
      <div class="text-xs font-semibold uppercase tracking-wider opacity-40 mb-1">Servers</div>

      <div v-if="error" role="alert" class="alert alert-error text-sm py-2">
        {{ error }}
      </div>

      <div
        v-for="server in servers"
        :key="server.addr"
        class="flex items-center justify-between bg-base-200 rounded-lg px-4 py-3"
      >
        <div>
          <div class="font-medium text-sm">{{ server.name }}</div>
          <div class="text-xs opacity-50 font-mono">{{ server.addr }}</div>
        </div>
        <button
          class="btn btn-primary btn-sm"
          :disabled="connecting"
          :aria-label="`Connect to ${server.name}`"
          @click="handleConnect(server.addr)"
        >
          {{ connectingAddr === server.addr ? 'Connecting...' : 'Connect' }}
        </button>
      </div>
    </div>
  </div>
</template>
