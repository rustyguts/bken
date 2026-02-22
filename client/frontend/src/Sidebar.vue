<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, nextTick, watch } from 'vue'
import { GetConfig, SaveConfig } from './config'
import type { ServerEntry } from './config'
import { BKEN_SCHEME } from './constants'
import { Home, Settings } from 'lucide-vue-next'

const props = defineProps<{
  activeServerAddr: string
  connectedAddr: string
  connected: boolean
  voiceConnected: boolean
  startupAddr: string
  globalUsername: string
}>()

const emit = defineEmits<{
  selectServer: [addr: string]
  goHome: []
  renameUsername: [username: string]
  openSettings: []
}>()

// Confirmation dialog state (when switching servers while connected)
const confirmDialog = ref(false)
const confirmTargetAddr = ref('')

const servers = ref<ServerEntry[]>([{ name: 'Local Dev', addr: 'localhost:8080' }])

function normalizeAddr(raw: string): string {
  let addr = raw.trim()
  if (addr.startsWith(BKEN_SCHEME)) addr = addr.slice(BKEN_SCHEME.length)
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
  return out
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

const avatarColors = [
  'bg-primary text-primary-content',
  'bg-secondary text-secondary-content',
  'bg-accent text-accent-content',
  'bg-info text-info-content',
  'bg-success text-success-content',
  'bg-warning text-warning-content',
] as const

function serverColorClass(name: string): string {
  let hash = 0
  for (const char of name.toLowerCase()) {
    hash = (hash * 31 + char.charCodeAt(0)) % avatarColors.length
  }
  return avatarColors[hash]
}

function selectServer(addr: string): void {
  const normalized = normalizeAddr(addr)
  if (isServerConnected(normalized)) {
    emit('selectServer', normalized)
    return
  }
  if (props.connected) {
    confirmTargetAddr.value = normalized
    confirmDialog.value = true
    return
  }
  emit('selectServer', normalized)
}

function confirmSwitch(): void {
  confirmDialog.value = false
  emit('selectServer', confirmTargetAddr.value)
  confirmTargetAddr.value = ''
}

function cancelSwitch(): void {
  confirmDialog.value = false
  confirmTargetAddr.value = ''
}

function isServerConnected(addr: string): boolean {
  return normalizeAddr(addr) === normalizeAddr(props.connectedAddr)
}

async function ensureStartupAddr(addr: string): Promise<void> {
  const clean = normalizeAddr(addr)
  if (!clean || servers.value.some(s => s.addr === clean)) return
  servers.value = [...servers.value, { name: 'Invited Server', addr: clean }]
  await saveConfig()
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
  closeServerContextMenu()
  await saveConfig()
  if (normalizeAddr(props.activeServerAddr) === addr) {
    emit('selectServer', servers.value[0].addr)
  }
}

// User dropdown menu
const userDropdownEl = ref<HTMLDetailsElement | null>(null)

function closeUserMenu(): void {
  if (userDropdownEl.value) {
    userDropdownEl.value.open = false
  }
}

// Username rename modal
const renameModalOpen = ref(false)
const renameInput = ref('')
const renameInputEl = ref<HTMLInputElement | null>(null)

async function openRenameModal(): Promise<void> {
  renameInput.value = props.globalUsername?.trim() ?? ''
  renameModalOpen.value = true
  await nextTick()
  renameInputEl.value?.focus()
  renameInputEl.value?.select()
}

function confirmRename(): void {
  const cleaned = renameInput.value.trim()
  if (!cleaned || cleaned === (props.globalUsername?.trim() ?? '')) {
    renameModalOpen.value = false
    return
  }
  emit('renameUsername', cleaned)
  renameModalOpen.value = false
}

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
    <aside class="relative flex flex-col items-center border-r border-base-content/10 bg-base-300 w-[64px] min-w-[64px] max-w-[64px] h-full" @click="closeServerContextMenu()">
      <div class="border-b border-base-content/10 h-12 w-full flex items-center justify-center shrink-0">
        <button
          class="btn btn-ghost btn-square btn-sm"
          aria-label="Home"
          title="Home"
          @click="emit('goHome')"
        >
          <Home class="w-5 h-5" aria-hidden="true" />
        </button>
      </div>

      <div class="flex-1 min-h-0 w-full overflow-y-auto overflow-x-hidden py-2 px-2">
        <div class="flex flex-col items-center gap-2">
          <div
            v-for="server in servers"
            :key="server.addr"
            class="avatar avatar-placeholder cursor-pointer relative transition-transform hover:scale-105"
            :title="`${server.name} (${server.addr})`"
            :aria-label="`Open ${server.name}`"
            @click="selectServer(server.addr)"
            @contextmenu="openServerContextMenu($event, server)"
          >
            <div class="w-8 rounded-full" :class="serverColorClass(server.name)">
              <span class="text-xs">{{ initials(server.name) }}</span>
            </div>
            <span
              v-if="isServerConnected(server.addr)"
              class="absolute -right-0.5 -bottom-0.5 w-2 h-2 rounded-full bg-success border border-base-100"
              aria-hidden="true"
            />
          </div>
        </div>
      </div>

      <!-- User avatar + dropdown at bottom -->
      <details ref="userDropdownEl" class="dropdown dropdown-right dropdown-end border-t border-base-content/10 p-2 shrink-0 flex items-center justify-center">
        <summary
          class="avatar avatar-placeholder cursor-pointer hover:ring-2 hover:ring-primary/40 transition-shadow rounded-full list-none"
          :title="globalUsername || 'User menu'"
        >
          <div class="bg-neutral text-neutral-content w-8 rounded-full">
            <span class="text-xs">{{ initials(globalUsername) }}</span>
          </div>
        </summary>
        <ul
          data-testid="user-menu"
          class="dropdown-content menu menu-sm bg-base-200 rounded-box shadow-lg border border-base-content/10 z-50 min-w-[160px]"
        >
          <li class="menu-title text-[10px]">{{ globalUsername }}</li>
          <li><a @click="openRenameModal(); closeUserMenu()">Rename Username</a></li>
          <li>
            <a @click="emit('openSettings'); closeUserMenu()">
              <Settings class="w-3.5 h-3.5" aria-hidden="true" />
              User Settings
            </a>
          </li>
        </ul>
      </details>
    </aside>

    <!-- Server right-click context menu -->
    <Teleport to="body">
      <ul
        v-if="serverContextMenu"
        class="menu menu-sm bg-base-200 rounded-box shadow-lg border border-base-content/10 fixed z-50 min-w-[140px]"
        :style="{ left: serverContextMenu.x + 'px', top: serverContextMenu.y + 'px' }"
        @click.stop
      >
        <li class="menu-title text-[10px] truncate max-w-[180px]">{{ serverContextMenu.server.name }}</li>
        <li><a class="text-error" @click="removeServer">Remove Server</a></li>
      </ul>
    </Teleport>

    <!-- Username rename modal -->
    <dialog class="modal" :class="{ 'modal-open': renameModalOpen }">
      <div class="modal-box w-80">
        <h3 class="text-lg font-bold">Set Username</h3>
        <div class="py-4">
          <input
            ref="renameInputEl"
            v-model="renameInput"
            type="text"
            placeholder="Enter username"
            class="input input-bordered w-full"
            maxlength="32"
            @keydown.enter.prevent="confirmRename"
          />
        </div>
        <div class="modal-action">
          <button class="btn btn-ghost" @click="renameModalOpen = false">Cancel</button>
          <button class="btn btn-soft btn-primary" :disabled="!renameInput.trim()" @click="confirmRename">Save</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="renameModalOpen = false">
        <button>close</button>
      </form>
    </dialog>

    <!-- Switch server confirmation dialog -->
    <dialog class="modal" :class="{ 'modal-open': confirmDialog }">
      <div class="modal-box w-80">
        <h3 class="text-sm font-semibold mb-2">Switch Server?</h3>
        <p v-if="voiceConnected" class="text-xs opacity-70">
          You're in a voice channel. Switching servers will disconnect you.
        </p>
        <p v-else class="text-xs opacity-70">
          You'll be disconnected from the current server.
        </p>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" @click="cancelSwitch">Cancel</button>
          <button class="btn btn-primary btn-sm" @click="confirmSwitch">Switch</button>
        </div>
      </div>
      <form method="dialog" class="modal-backdrop" @click="cancelSwitch">
        <button>close</button>
      </form>
    </dialog>
  </div>
</template>
