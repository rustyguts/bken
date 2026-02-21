<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { GetConfig, SaveConfig, SetPTTMode } from './config'
import { Terminal } from 'lucide-vue-next'

const pttEnabled = ref(false)
const pttKey = ref('Backquote')
const rebindingPTT = ref(false)

/** Human-readable label for a KeyboardEvent.code value. */
function keyLabel(code: string): string {
  const labels: Record<string, string> = {
    Backquote: '`',
    Space: 'Space',
    CapsLock: 'CapsLock',
    Tab: 'Tab',
    ShiftLeft: 'L-Shift',
    ShiftRight: 'R-Shift',
    ControlLeft: 'L-Ctrl',
    ControlRight: 'R-Ctrl',
    AltLeft: 'L-Alt',
    AltRight: 'R-Alt',
  }
  return labels[code] ?? code.replace(/^Key/, '').replace(/^Digit/, '')
}

async function persistConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    ptt_enabled: pttEnabled.value,
    ptt_key: pttKey.value,
  })
}

async function handlePTTToggle(): Promise<void> {
  await SetPTTMode(pttEnabled.value)
  await persistConfig()
  window.dispatchEvent(new CustomEvent('ptt-config-changed', {
    detail: { enabled: pttEnabled.value, key: pttKey.value },
  }))
}

function startRebindPTT(): void {
  rebindingPTT.value = true
  function onKey(e: KeyboardEvent) {
    e.preventDefault()
    e.stopPropagation()
    pttKey.value = e.code
    rebindingPTT.value = false
    window.removeEventListener('keydown', onKey, true)
    SetPTTMode(pttEnabled.value)
    persistConfig()
    window.dispatchEvent(new CustomEvent('ptt-config-changed', {
      detail: { enabled: pttEnabled.value, key: pttKey.value },
    }))
  }
  window.addEventListener('keydown', onKey, true)
}

onMounted(async () => {
  const cfg = await GetConfig()
  pttEnabled.value = cfg.ptt_enabled ?? false
  pttKey.value = cfg.ptt_key || 'Backquote'
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <Terminal class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Key Bindings</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4 flex flex-col gap-4">

      <!-- Push-to-Talk -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Push to Talk</p>
            <p class="text-xs opacity-50 mt-0.5">Hold a key to transmit</p>
          </div>
          <input
            type="checkbox"
            v-model="pttEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle push-to-talk"
            @change="handlePTTToggle"
          />
        </div>
        <div class="mt-3 transition-opacity" :class="{ 'opacity-30 pointer-events-none': !pttEnabled }">
          <div class="flex items-center justify-between">
            <span class="text-xs opacity-70">Key</span>
            <button
              class="btn btn-xs btn-outline font-mono min-w-[4rem]"
              :class="{ 'btn-primary animate-pulse': rebindingPTT }"
              :disabled="!pttEnabled"
              @click="startRebindPTT"
            >
              {{ rebindingPTT ? '...' : keyLabel(pttKey) }}
            </button>
          </div>
          <p class="text-xs opacity-40 mt-2">Click the button above, then press any key to rebind. Works when the app is focused.</p>
        </div>
      </div>
    </div>
  </section>
</template>
