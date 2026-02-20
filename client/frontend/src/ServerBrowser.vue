<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{
  connect: [payload: { username: string; addr: string }]
}>()

const username = ref('')
const error = ref('')
const connecting = ref(false)
const connectingAddr = ref('')

const servers = ref([
  { name: 'Local Dev', addr: 'localhost:4433' },
])

function handleConnect(addr: string) {
  if (!username.value.trim()) {
    error.value = 'Please enter a username'
    return
  }
  connecting.value = true
  connectingAddr.value = addr
  error.value = ''
  emit('connect', { username: username.value.trim(), addr })
}

function setError(msg: string) {
  error.value = msg
  connecting.value = false
  connectingAddr.value = ''
}

defineExpose({ setError })
</script>

<template>
  <div class="flex flex-col items-center justify-center h-full p-8 gap-8">
    <div class="text-center">
      <h1 class="text-4xl font-bold tracking-widest">bken</h1>
      <p class="opacity-60 mt-1 text-sm">Voice Communication</p>
    </div>

    <!-- Username -->
    <div class="w-80">
      <label class="form-control w-full">
        <div class="label"><span class="label-text">Username</span></div>
        <input
          type="text"
          v-model="username"
          placeholder="Enter username"
          class="input input-bordered w-full"
          :disabled="connecting"
          @keydown.enter="servers.length === 1 && handleConnect(servers[0].addr)"
        />
      </label>
    </div>

    <!-- Server list -->
    <div class="w-80 flex flex-col gap-2">
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
          @click="handleConnect(server.addr)"
        >
          {{ connectingAddr === server.addr ? 'Connecting…' : 'Connect' }}
        </button>
      </div>
    </div>
  </div>
</template>
