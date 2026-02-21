<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { SetNoiseSuppression } from '../wailsjs/go/main/App'
import { GetConfig, SaveConfig, SetAEC, SetAGC } from './config'
import { ShieldCheck, Waves, Mic2 } from 'lucide-vue-next'

const aecEnabled = ref(true)
const noiseEnabled = ref(true)
const agcEnabled = ref(true)

async function persistConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    aec_enabled: aecEnabled.value,
    noise_enabled: noiseEnabled.value,
    agc_enabled: agcEnabled.value,
  })
}

async function handleAECToggle(): Promise<void> {
  await SetAEC(aecEnabled.value)
  await persistConfig()
}

async function handleNoiseToggle(): Promise<void> {
  await SetNoiseSuppression(noiseEnabled.value)
  await persistConfig()
}

async function handleAGCToggle(): Promise<void> {
  await SetAGC(agcEnabled.value)
  await persistConfig()
}

onMounted(async () => {
  const cfg = await GetConfig()
  aecEnabled.value = cfg.aec_enabled ?? true
  noiseEnabled.value = cfg.noise_enabled ?? true
  agcEnabled.value = cfg.agc_enabled ?? true
})
</script>

<template>
  <section class="card border border-base-content/10 bg-base-100 shadow-sm">
    <div class="card-body gap-4">
      <div class="flex items-start justify-between gap-3">
        <div>
          <p class="text-xs font-semibold uppercase tracking-wider text-primary/80">Voice Enhancements</p>
          <h3 class="text-lg font-semibold leading-tight">Help Others Hear You Clearly</h3>
          <p class="text-sm opacity-70 mt-1">Turn these on for clearer calls in most channels.</p>
        </div>
      </div>

      <div class="divider my-0"></div>

      <div class="grid gap-3">
        <label class="flex items-center justify-between gap-3 rounded-xl border border-base-content/10 bg-base-200/40 px-3 py-3">
          <div class="flex items-center gap-3">
            <div class="size-9 rounded-lg bg-primary/10 text-primary grid place-items-center">
              <ShieldCheck class="size-4" aria-hidden="true" />
            </div>
            <div>
              <p class="text-sm font-medium leading-none">Echo Cancellation</p>
              <p class="text-xs opacity-60 mt-1">Helps stop speaker sound from feeding back into your mic.</p>
            </div>
          </div>
          <input
            v-model="aecEnabled"
            type="checkbox"
            class="toggle toggle-primary"
            aria-label="Toggle echo cancellation"
            @change="handleAECToggle"
          />
        </label>

        <label class="flex items-center justify-between gap-3 rounded-xl border border-base-content/10 bg-base-200/40 px-3 py-3">
          <div class="flex items-center gap-3">
            <div class="size-9 rounded-lg bg-primary/10 text-primary grid place-items-center">
              <Waves class="size-4" aria-hidden="true" />
            </div>
            <div>
              <p class="text-sm font-medium leading-none">Noise Suppression</p>
              <p class="text-xs opacity-60 mt-1">Reduces steady background sounds like fans and hum.</p>
            </div>
          </div>
          <input
            v-model="noiseEnabled"
            type="checkbox"
            class="toggle toggle-primary"
            aria-label="Toggle noise suppression"
            @change="handleNoiseToggle"
          />
        </label>

        <label class="flex items-center justify-between gap-3 rounded-xl border border-base-content/10 bg-base-200/40 px-3 py-3">
          <div class="flex items-center gap-3">
            <div class="size-9 rounded-lg bg-primary/10 text-primary grid place-items-center">
              <Mic2 class="size-4" aria-hidden="true" />
            </div>
            <div>
              <p class="text-sm font-medium leading-none">Volume Normalization</p>
              <p class="text-xs opacity-60 mt-1">Keeps your voice at a more even loudness over time.</p>
            </div>
          </div>
          <input
            v-model="agcEnabled"
            type="checkbox"
            class="toggle toggle-primary"
            aria-label="Toggle volume normalization"
            @change="handleAGCToggle"
          />
        </label>
      </div>
    </div>
  </section>
</template>
