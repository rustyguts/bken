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

// Add-server form state
const showAddForm = ref(false)
const newName = ref('')
const newAddr = ref('')
const addError = ref('')

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

async function addServer(): Promise<void> {
  const name = newName.value.trim()
  const addr = newAddr.value.trim()
  if (!name || !addr) {
    addError.value = 'Name and address are required'
    return
  }
  addError.value = ''
  servers.value = [...servers.value, { name, addr }]
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, servers: servers.value })
  newName.value = ''
  newAddr.value = ''
  showAddForm.value = false
}

async function removeServer(addr: string): Promise<void> {
  servers.value = servers.value.filter(s => s.addr !== addr)
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, servers: servers.value })
}

function cancelAdd(): void {
  showAddForm.value = false
  newName.value = ''
  newAddr.value = ''
  addError.value = ''
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
      <!-- Header row with add button -->
      <div class="flex items-center mb-1">
        <span class="text-xs font-semibold uppercase tracking-wider opacity-40 flex-1">Servers</span>
        <button
          class="btn btn-ghost btn-xs opacity-50 hover:opacity-100"
          :disabled="connecting"
          aria-label="Add server"
          title="Add server"
          @click="showAddForm = !showAddForm"
        >
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4" aria-hidden="true">
            <path d="M10.75 4.75a.75.75 0 0 0-1.5 0v4.5h-4.5a.75.75 0 0 0 0 1.5h4.5v4.5a.75.75 0 0 0 1.5 0v-4.5h4.5a.75.75 0 0 0 0-1.5h-4.5v-4.5Z" />
          </svg>
        </button>
      </div>

      <!-- Connection error -->
      <div v-if="error" role="alert" class="alert alert-error text-sm py-2">
        {{ error }}
      </div>

      <!-- Add server form -->
      <Transition name="fade">
        <div v-if="showAddForm" class="bg-base-200 rounded-lg p-3 flex flex-col gap-2">
          <div class="text-xs font-semibold opacity-60">Add Server</div>
          <input
            v-model="newName"
            type="text"
            placeholder="Name"
            class="input input-sm input-bordered w-full"
            :disabled="connecting"
            @keydown.enter="addServer"
          />
          <input
            v-model="newAddr"
            type="text"
            placeholder="host:port"
            class="input input-sm input-bordered w-full font-mono"
            :disabled="connecting"
            @keydown.enter="addServer"
          />
          <div v-if="addError" class="text-error text-xs">{{ addError }}</div>
          <div class="flex gap-2">
            <button class="btn btn-primary btn-sm flex-1" :disabled="connecting" @click="addServer">Add</button>
            <button class="btn btn-ghost btn-sm" @click="cancelAdd">Cancel</button>
          </div>
        </div>
      </Transition>

      <!-- Server entries -->
      <div
        v-for="server in servers"
        :key="server.addr"
        class="flex items-center justify-between bg-base-200 rounded-lg px-4 py-3 gap-2"
      >
        <div class="flex-1 min-w-0">
          <div class="font-medium text-sm truncate">{{ server.name }}</div>
          <div class="text-xs opacity-50 font-mono truncate">{{ server.addr }}</div>
        </div>
        <div class="flex gap-1 shrink-0">
          <button
            class="btn btn-primary btn-sm"
            :disabled="connecting"
            :aria-label="`Connect to ${server.name}`"
            @click="handleConnect(server.addr)"
          >
            {{ connectingAddr === server.addr ? 'Connecting…' : 'Connect' }}
          </button>
          <button
            class="btn btn-ghost btn-sm btn-square opacity-50 hover:opacity-100 hover:text-error"
            :disabled="connecting"
            :aria-label="`Remove ${server.name}`"
            title="Remove server"
            @click="removeServer(server.addr)"
          >
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4" aria-hidden="true">
              <path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 0 0 6 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 1 0 .23 1.482l.149-.022.841 10.518A2.75 2.75 0 0 0 7.596 19h4.807a2.75 2.75 0 0 0 2.742-2.53l.841-10.52.149.023a.75.75 0 0 0 .23-1.482A41.03 41.03 0 0 0 14 4.193V3.75A2.75 2.75 0 0 0 11.25 1h-2.5Zm0 1.5c-.69 0-1.25.56-1.25 1.25v.25h5v-.25c0-.69-.56-1.25-1.25-1.25h-2.5Zm5.31 6.5H5.94l.688 8.6A1.25 1.25 0 0 0 7.596 17.5h4.807a1.25 1.25 0 0 0 1.245-1.15l.689-8.6Z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>
      </div>

      <div v-if="servers.length === 0" class="text-xs opacity-40 italic text-center py-3">
        No servers — click <span class="font-semibold not-italic">+</span> to add one
      </div>
    </div>
  </div>
</template>
