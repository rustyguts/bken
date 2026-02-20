<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { GetConfig, SaveConfig } from './config'
import type { ServerEntry } from './config'
import type { ConnectPayload } from './types'

const props = defineProps<{
  activeServerAddr: string
  connectedAddr: string
  connectError: string
  startupAddr: string
  globalUsername: string
}>()

const emit = defineEmits<{
  connect: [payload: ConnectPayload]
  selectServer: [addr: string]
}>()

const browserOpen = ref(false)
const connectingAddr = ref('')

const newName = ref('')
const newAddr = ref('')
const browserError = ref('')

const servers = ref<ServerEntry[]>([{ name: 'Local Dev', addr: 'localhost:4433' }])

function normalizeAddr(raw: string): string {
  let addr = raw.trim()
  if (addr.startsWith('bken://')) addr = addr.slice('bken://'.length)
  return addr
}

function normalizeServers(entries: ServerEntry[]): ServerEntry[] {
  const out: ServerEntry[] = []
  const seen = new Set<string>()
  for (const server of entries) {
    const addr = normalizeAddr(server.addr)
    if (!addr || seen.has(addr)) continue
    seen.add(addr)
    out.push({
      name: server.name?.trim() || addr,
      addr,
    })
  }
  return out.length ? out : [{ name: 'Local Dev', addr: 'localhost:4433' }]
}

async function loadConfig(): Promise<void> {
  const cfg = await GetConfig()
  if (cfg.servers?.length) {
    servers.value = normalizeServers(cfg.servers)
  }
}

async function saveConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    servers: normalizeServers(servers.value),
  })
}

function initials(name: string): string {
  const first = name.trim()[0]
  return first ? first.toUpperCase() : '?'
}

function nameHue(name: string): number {
  let hash = 0
  for (const char of name.toLowerCase()) {
    hash = (hash * 31 + char.charCodeAt(0)) % 360
  }
  return hash
}

function serverButtonStyle(name: string, active: boolean): Record<string, string> {
  const hue = nameHue(name)
  if (active) {
    return {
      backgroundColor: `hsl(${hue} 65% 45%)`,
      borderColor: `hsl(${hue} 70% 28%)`,
      color: 'white',
    }
  }
  return {
    backgroundColor: `hsl(${hue} 65% 86%)`,
    borderColor: `hsl(${hue} 45% 64%)`,
    color: `hsl(${hue} 60% 24%)`,
  }
}

function selectServer(addr: string): void {
  emit('selectServer', normalizeAddr(addr))
}

async function connectToNewServer(): Promise<void> {
  const user = props.globalUsername.trim()
  const addr = normalizeAddr(newAddr.value)
  const name = newName.value.trim() || addr

  if (!user) {
    browserError.value = 'Set your global username in User Controls first.'
    return
  }
  if (!addr) {
    browserError.value = 'Server address is required'
    return
  }

  const idx = servers.value.findIndex(s => s.addr === addr)
  if (idx >= 0) {
    const updated = [...servers.value]
    updated[idx] = { ...updated[idx], name: updated[idx].name || name }
    servers.value = updated
  } else {
    servers.value = [...servers.value, { name, addr }]
  }

  browserError.value = ''
  connectingAddr.value = addr
  await saveConfig()

  emit('selectServer', addr)
  emit('connect', { username: user, addr })
}

async function ensureStartupAddr(addr: string): Promise<void> {
  const clean = normalizeAddr(addr)
  if (!clean || servers.value.some(s => s.addr === clean)) return
  servers.value = [...servers.value, { name: 'Invited Server', addr: clean }]
  await saveConfig()
  if (!newAddr.value.trim()) newAddr.value = clean
}

watch(() => props.connectedAddr, () => {
  connectingAddr.value = ''
  browserOpen.value = false
})

watch(() => props.connectError, (msg) => {
  if (msg) connectingAddr.value = ''
})

watch(() => props.startupAddr, (addr) => {
  void ensureStartupAddr(addr)
})

onMounted(async () => {
  await loadConfig()
  await ensureStartupAddr(props.startupAddr)
})
</script>

<template>
  <div class="h-full min-h-0">
    <aside class="relative flex flex-col items-center border-r border-base-content/10 bg-base-300 py-3 px-2 gap-2 w-[64px] min-w-[64px] max-w-[64px] h-full overflow-x-hidden">
      <button
        class="btn btn-ghost btn-square btn-sm"
        aria-label="Server browser"
        :class="browserOpen ? 'text-primary' : 'opacity-70 hover:opacity-100'"
        @click="browserOpen = true"
      >
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z" />
        </svg>
      </button>

      <div class="w-full border-t border-base-content/20 my-1" />

      <div class="flex-1 min-h-0 w-full overflow-y-auto overflow-x-hidden">
        <div class="flex flex-col items-center gap-2 mt-1">
          <button
            v-for="server in servers"
            :key="server.addr"
            class="relative w-8 h-8 rounded-full border text-[9px] font-mono font-semibold transition-all hover:scale-105"
            :style="serverButtonStyle(server.name, normalizeAddr(server.addr) === normalizeAddr(activeServerAddr))"
            :title="`${server.name} (${server.addr})`"
            :aria-label="`Open ${server.name}`"
            :class="normalizeAddr(server.addr) === normalizeAddr(activeServerAddr) ? 'ring-2 ring-offset-1 ring-base-content/35' : ''"
            @click="selectServer(server.addr)"
          >
            {{ initials(server.name) }}
            <span
              v-if="normalizeAddr(server.addr) === normalizeAddr(connectedAddr)"
              class="absolute -right-0.5 -bottom-0.5 w-2 h-2 rounded-full bg-success border border-base-100"
              aria-hidden="true"
            />
          </button>
        </div>
      </div>
    </aside>

    <dialog class="modal" :class="{ 'modal-open': browserOpen }">
      <div class="modal-box w-11/12 max-w-md">
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold uppercase tracking-wider opacity-70">Connect To New Server</h3>
          <button class="btn btn-ghost btn-xs" aria-label="Close server browser" @click="browserOpen = false">✕</button>
        </div>

        <p class="text-xs opacity-60 mb-3">Connects with your global username: <span class="font-semibold">{{ globalUsername || 'not set' }}</span></p>

        <div class="space-y-2">
          <input
            v-model="newName"
            type="text"
            placeholder="Server name (optional)"
            class="input input-sm input-bordered w-full"
          />
          <input
            v-model="newAddr"
            type="text"
            placeholder="host:port or bken:// link"
            class="input input-sm input-bordered w-full font-mono"
            @keydown.enter.prevent="connectToNewServer"
          />
        </div>

        <div v-if="connectError" class="alert alert-error py-2 text-sm mt-2">{{ connectError }}</div>
        <div v-else-if="browserError" class="alert alert-error py-2 text-sm mt-2">{{ browserError }}</div>

        <div class="mt-3 flex gap-2">
          <button class="btn btn-soft btn-primary btn-sm flex-1" @click="connectToNewServer">
            {{ connectingAddr ? 'Connecting…' : 'Connect' }}
          </button>
          <button class="btn btn-ghost btn-sm" @click="browserOpen = false">Close</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="browserOpen = false">
        <button>close</button>
      </form>
    </dialog>
  </div>
</template>
