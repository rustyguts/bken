<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { GetConfig, SaveConfig } from './config'
import type { ServerEntry } from './config'
import type { ConnectPayload } from './types'
import { Server } from 'lucide-vue-next'

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

const servers = ref<ServerEntry[]>([{ name: 'Local Dev', addr: 'localhost:8443' }])

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
  return out.length ? out : [{ name: 'Local Dev', addr: 'localhost:8443' }]
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

// Server context menu
const serverContextMenu = ref<{ x: number; y: number; server: ServerEntry } | null>(null)

function openServerContextMenu(event: MouseEvent, server: ServerEntry): void {
  event.preventDefault()
  serverContextMenu.value = { x: event.clientX, y: event.clientY, server }
}

function closeServerContextMenu(): void {
  serverContextMenu.value = null
}

async function removeServer(): Promise<void> {
  if (!serverContextMenu.value) return
  const addr = normalizeAddr(serverContextMenu.value.server.addr)
  servers.value = servers.value.filter(s => normalizeAddr(s.addr) !== addr)
  if (!servers.value.length) {
    servers.value = [{ name: 'Local Dev', addr: 'localhost:8443' }]
  }
  closeServerContextMenu()
  await saveConfig()
  // If the removed server was the active one, deselect it
  if (normalizeAddr(props.activeServerAddr) === addr) {
    emit('selectServer', servers.value[0].addr)
  }
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

function handleGlobalClick(): void {
  closeServerContextMenu()
}

onMounted(async () => {
  document.addEventListener('click', handleGlobalClick)
  await loadConfig()
  await ensureStartupAddr(props.startupAddr)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleGlobalClick)
})
</script>

<template>
  <div class="h-full min-h-0">
    <aside class="relative flex flex-col items-center border-r border-base-content/10 bg-base-300 w-[64px] min-w-[64px] max-w-[64px] h-full overflow-x-hidden" @click="closeServerContextMenu()">
      <div class="border-b border-base-content/10 min-h-11 w-full flex items-center justify-center shrink-0">
        <button
          class="btn btn-ghost btn-square btn-sm"
          aria-label="Server browser"
          :class="browserOpen ? 'text-primary' : 'opacity-70 hover:opacity-100'"
          @click="browserOpen = true"
        >
          <Server class="w-5 h-5" aria-hidden="true" />
        </button>
      </div>

      <div class="flex-1 min-h-0 w-full overflow-y-auto overflow-x-hidden py-2 px-2">
        <div class="flex flex-col items-center gap-2">
          <button
            v-for="server in servers"
            :key="server.addr"
            class="relative w-8 h-8 rounded-full border text-[9px] font-mono font-semibold transition-all hover:scale-105"
            :style="serverButtonStyle(server.name, normalizeAddr(server.addr) === normalizeAddr(activeServerAddr))"
            :title="`${server.name} (${server.addr})`"
            :aria-label="`Open ${server.name}`"
            :class="normalizeAddr(server.addr) === normalizeAddr(activeServerAddr) ? 'ring-2 ring-offset-1 ring-base-content/35' : ''"
            @click="selectServer(server.addr)"
            @contextmenu="openServerContextMenu($event, server)"
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

    <!-- Server right-click context menu -->
    <Teleport to="body">
      <div
        v-if="serverContextMenu"
        class="fixed z-50 min-w-[140px] rounded-lg border border-base-content/15 bg-base-200 shadow-lg py-1"
        :style="{ left: serverContextMenu.x + 'px', top: serverContextMenu.y + 'px' }"
        @click.stop
      >
        <div class="px-3 py-1 text-[10px] uppercase tracking-wider opacity-40 select-none truncate max-w-[180px]">
          {{ serverContextMenu.server.name }}
        </div>
        <button
          class="w-full text-left px-3 py-1.5 text-xs text-error hover:bg-error/10 transition-colors"
          @click="removeServer"
        >
          Remove Server
        </button>
      </div>
    </Teleport>

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
