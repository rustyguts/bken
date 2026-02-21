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
  <section>
    <div class="flex items-center gap-2 mb-3">
      <ShieldCheck class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Voice Enhancements</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10">
      <div class="card-body gap-4 p-4">
        <div>
          <h3 class="card-title text-sm">Help Others Hear You Clearly</h3>
          <p class="text-xs opacity-70 mt-1">Turn these on for clearer calls in most channels.</p>
        </div>

        <div class="divider my-0"></div>

        <fieldset class="fieldset">
          <legend class="fieldset-legend text-xs">Processing</legend>
          <div class="grid gap-3">
            <label class="label cursor-pointer justify-between gap-3 rounded-lg border border-base-content/10 bg-base-200/40 px-3 py-3">
              <div class="flex items-center gap-3">
                <div class="avatar placeholder">
                  <div class="bg-primary/10 text-primary w-9 rounded-lg">
                    <ShieldCheck class="size-4" aria-hidden="true" />
                  </div>
                </div>
                <div>
                  <span class="label-text text-sm font-medium">Echo Cancellation</span>
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

            <label class="label cursor-pointer justify-between gap-3 rounded-lg border border-base-content/10 bg-base-200/40 px-3 py-3">
              <div class="flex items-center gap-3">
                <div class="avatar placeholder">
                  <div class="bg-primary/10 text-primary w-9 rounded-lg">
                    <Waves class="size-4" aria-hidden="true" />
                  </div>
                </div>
                <div>
                  <span class="label-text text-sm font-medium">Noise Suppression</span>
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

            <label class="label cursor-pointer justify-between gap-3 rounded-lg border border-base-content/10 bg-base-200/40 px-3 py-3">
              <div class="flex items-center gap-3">
                <div class="avatar placeholder">
                  <div class="bg-primary/10 text-primary w-9 rounded-lg">
                    <Mic2 class="size-4" aria-hidden="true" />
                  </div>
                </div>
                <div>
                  <span class="label-text text-sm font-medium">Volume Normalization</span>
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
        </fieldset>
      </div>
    </div>
  </section>
</template>
