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

    <div class="card bg-base-200/40 border border-base-content/10">
      <div class="card-body gap-4 p-4">
        <fieldset class="fieldset">
          <legend class="fieldset-legend text-xs">Push to Talk</legend>
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium leading-none">Enable PTT</p>
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
        </fieldset>

        <fieldset class="fieldset transition-opacity" :class="{ 'opacity-30 pointer-events-none': !pttEnabled }">
          <legend class="fieldset-legend text-xs">PTT Key</legend>
          <div class="flex items-center justify-between">
            <span class="text-xs opacity-70">Key</span>
            <button
              class="btn btn-xs btn-outline min-w-[4rem]"
              :class="{ 'btn-primary animate-pulse': rebindingPTT }"
              :disabled="!pttEnabled"
              @click="startRebindPTT"
            >
              <kbd v-if="!rebindingPTT" class="kbd kbd-sm">{{ keyLabel(pttKey) }}</kbd>
              <span v-else>...</span>
            </button>
          </div>
          <p class="text-xs opacity-40 mt-2">Click the button above, then press any key to rebind. Works when the app is focused.</p>
        </fieldset>
      </div>
    </div>
  </section>
</template>
