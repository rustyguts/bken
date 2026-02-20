<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{
  connect: [payload: { username: string; addr: string }]
}>()

const username = ref('')
const serverAddr = ref('localhost:4433')
const error = ref('')
const connecting = ref(false)

function handleConnect() {
  if (!username.value.trim()) {
    error.value = 'Please enter a username'
    return
  }
  connecting.value = true
  error.value = ''
  emit('connect', { username: username.value.trim(), addr: serverAddr.value.trim() })
}

function setError(msg: string) {
  error.value = msg
  connecting.value = false
}

defineExpose({ setError })
</script>

<template>
  <div class="flex flex-col items-center justify-center h-full p-8">
    <h1 class="text-4xl font-bold tracking-widest">bken</h1>
    <p class="opacity-60 mt-1 mb-8 text-sm">Voice Communication</p>

    <div class="card bg-base-200 w-80 shadow-lg">
      <div class="card-body gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Username</span></div>
          <input
            type="text"
            v-model="username"
            placeholder="Enter username"
            class="input input-bordered w-full"
            :disabled="connecting"
            @keydown.enter="handleConnect"
          />
        </label>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Server</span></div>
          <input
            type="text"
            v-model="serverAddr"
            placeholder="host:port"
            class="input input-bordered w-full"
            :disabled="connecting"
            @keydown.enter="handleConnect"
          />
        </label>

        <div v-if="error" role="alert" class="alert alert-error text-sm py-2">
          {{ error }}
        </div>

        <button class="btn btn-primary w-full" :disabled="connecting" @click="handleConnect">
          {{ connecting ? 'Connecting...' : 'Connect' }}
        </button>
      </div>
    </div>
  </div>
</template>
