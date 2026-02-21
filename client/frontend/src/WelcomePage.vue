<script setup lang="ts">
import { ref, watch } from 'vue'
import { GetConfig, SaveConfig } from './config'
import type { ServerEntry } from './config'
import type { ConnectPayload } from './types'
import { BKEN_SCHEME } from './constants'
import { Server, X } from 'lucide-vue-next'

const props = defineProps<{
  servers: ServerEntry[]
  globalUsername: string
  connectError: string
  startupAddr: string
}>()

const emit = defineEmits<{
  connect: [payload: ConnectPayload]
}>()

const newName = ref('')
const newAddr = ref('')
const error = ref('')
const connectingAddr = ref('')

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
  connectingAddr.value = normalized
  emit('connect', { username: user, addr: normalized })
}

async function connectToNewServer(): Promise<void> {
  const user = props.globalUsername.trim()
  const addr = normalizeAddr(newAddr.value)
  const name = newName.value.trim() || addr

  if (!user) {
    error.value = 'Set your username in settings first.'
    return
  }
  if (!addr) {
    error.value = 'Server address is required.'
    return
  }

  // Persist the new server to saved list
  const cfg = await GetConfig()
  const servers = cfg.servers?.length ? [...cfg.servers] : []
  const idx = servers.findIndex(s => normalizeAddr(s.addr) === addr)
  if (idx >= 0) {
    servers[idx] = { ...servers[idx], name: servers[idx].name || name }
  } else {
    servers.push({ name, addr })
  }
  await SaveConfig({ ...cfg, servers })

  error.value = ''
  connectingAddr.value = addr
  emit('connect', { username: user, addr })
}

async function removeServer(addr: string): Promise<void> {
  const normalized = normalizeAddr(addr)
  const cfg = await GetConfig()
  const servers = (cfg.servers ?? []).filter(s => normalizeAddr(s.addr) !== normalized)
  await SaveConfig({ ...cfg, servers: servers.length ? servers : [{ name: 'Local Dev', addr: 'localhost:8080' }] })
}

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
}

watch(() => props.startupAddr, (addr) => {
  if (addr && !newAddr.value.trim()) {
    newAddr.value = normalizeAddr(addr)
  }
}, { immediate: true })

watch(() => props.connectError, (msg) => {
  if (msg) connectingAddr.value = ''
})
</script>

<template>
  <div class="hero h-full bg-base-100">
    <div class="hero-content flex-col">
      <div class="text-center space-y-2">
        <div class="avatar placeholder">
          <div class="bg-primary/10 text-primary w-16 rounded-2xl">
            <Server class="w-8 h-8" aria-hidden="true" />
          </div>
        </div>
        <h1 class="text-2xl font-bold">bken</h1>
        <p class="text-sm opacity-60">Select a saved server or connect to a new one.</p>
      </div>

      <div class="w-full max-w-md space-y-6">
        <div v-if="servers.length > 0">
          <p class="text-xs font-semibold uppercase tracking-wider opacity-50 mb-2">Saved Servers</p>
          <ul class="menu menu-sm bg-base-200 rounded-box">
            <li
              v-for="server in servers"
              :key="server.addr"
            >
              <a class="flex items-center gap-3" @click="connectToServer(server.addr)">
                <div class="avatar placeholder">
                  <div class="bg-neutral text-neutral-content w-6 rounded-full">
                    <span class="text-[9px]">{{ initials(server.name) }}</span>
                  </div>
                </div>
                <span class="flex-1 truncate">{{ server.name }}</span>
                <span class="badge badge-ghost badge-xs font-mono">{{ server.addr }}</span>
                <button
                  class="btn btn-ghost btn-xs opacity-30 hover:opacity-100 hover:text-error"
                  title="Remove server"
                  @click.stop="removeServer(server.addr)"
                >
                  <X class="w-3 h-3" aria-hidden="true" />
                </button>
              </a>
            </li>
          </ul>
        </div>

        <fieldset class="fieldset">
          <legend class="fieldset-legend">Connect to New Server</legend>
          <input
            v-model="newName"
            type="text"
            placeholder="Server name (optional)"
            class="input input-sm w-full"
          />
          <input
            v-model="newAddr"
            type="text"
            placeholder="host:port or bken:// link"
            class="input input-sm w-full font-mono"
            @keydown.enter.prevent="connectToNewServer"
          />
          <button
            class="btn btn-soft btn-primary btn-sm w-full"
            @click="connectToNewServer"
          >
            {{ connectingAddr ? 'Connecting...' : 'Connect' }}
          </button>
        </fieldset>

        <div v-if="connectError" role="alert" class="alert alert-error text-sm">{{ connectError }}</div>
        <div v-else-if="error" role="alert" class="alert alert-error text-sm">{{ error }}</div>
      </div>
    </div>
  </div>
</template>
