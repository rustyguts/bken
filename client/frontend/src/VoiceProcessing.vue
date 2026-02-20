<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  SetNoiseSuppression,
  SetNoiseSuppressionLevel,
} from '../wailsjs/go/main/App'
import { GetConfig, SaveConfig, SetAEC, SetAGC, SetAGCLevel, SetVAD, SetVADThreshold } from './config'

const aecEnabled = ref(true)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
const agcEnabled = ref(true)
const agcLevel = ref(50)
const vadEnabled = ref(true)
const vadThreshold = ref(30)

async function persistConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    aec_enabled: aecEnabled.value,
    noise_enabled: noiseEnabled.value,
    noise_level: noiseLevel.value,
    agc_enabled: agcEnabled.value,
    agc_level: agcLevel.value,
    vad_enabled: vadEnabled.value,
    vad_threshold: vadThreshold.value,
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

async function handleNoiseLevelChange(): Promise<void> {
  await SetNoiseSuppressionLevel(noiseLevel.value)
  await persistConfig()
}

async function handleAGCToggle(): Promise<void> {
  await SetAGC(agcEnabled.value)
  await persistConfig()
}

async function handleAGCLevelChange(): Promise<void> {
  await SetAGCLevel(agcLevel.value)
  await persistConfig()
}

async function handleVADToggle(): Promise<void> {
  await SetVAD(vadEnabled.value)
  await persistConfig()
}

async function handleVADThresholdChange(): Promise<void> {
  await SetVADThreshold(vadThreshold.value)
  await persistConfig()
}

onMounted(async () => {
  const cfg = await GetConfig()
  aecEnabled.value = cfg.aec_enabled
  noiseEnabled.value = cfg.noise_enabled
  noiseLevel.value = cfg.noise_level
  agcEnabled.value = cfg.agc_enabled
  agcLevel.value = cfg.agc_level
  vadEnabled.value = cfg.vad_enabled
  vadThreshold.value = cfg.vad_threshold
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
        <path stroke-linecap="round" stroke-linejoin="round" d="M10.5 6h9.75M10.5 6a1.5 1.5 0 11-3 0m3 0a1.5 1.5 0 10-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-9.75 0h9.75" />
      </svg>
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Voice Processing</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4 flex flex-col gap-4">

      <!-- Echo Cancellation -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Echo Cancellation</p>
            <p class="text-xs opacity-50 mt-0.5">Remove speaker feedback from mic</p>
          </div>
          <input
            type="checkbox"
            v-model="aecEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle echo cancellation"
            @change="handleAECToggle"
          />
        </div>
      </div>

      <div class="divider my-0 opacity-30"></div>

      <!-- Noise Suppression -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Noise Suppression</p>
            <p class="text-xs opacity-50 mt-0.5">Filter background noise from mic</p>
          </div>
          <input
            type="checkbox"
            v-model="noiseEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle noise suppression"
            @change="handleNoiseToggle"
          />
        </div>
        <div class="mt-3 transition-opacity" :class="{ 'opacity-30 pointer-events-none': !noiseEnabled }">
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs opacity-70">Strength</span>
            <span class="text-xs font-mono font-medium tabular-nums">{{ noiseLevel }}%</span>
          </div>
          <input
            type="range"
            v-model.number="noiseLevel"
            min="0"
            max="100"
            class="range range-xs range-primary w-full"
            :aria-label="`Noise suppression level: ${noiseLevel}%`"
            :disabled="!noiseEnabled"
            @input="handleNoiseLevelChange"
          />
        </div>
      </div>

      <div class="divider my-0 opacity-30"></div>

      <!-- Auto Gain Control -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Auto Gain Control</p>
            <p class="text-xs opacity-50 mt-0.5">Normalise mic volume automatically</p>
          </div>
          <input
            type="checkbox"
            v-model="agcEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle automatic gain control"
            @change="handleAGCToggle"
          />
        </div>
        <div class="mt-3 transition-opacity" :class="{ 'opacity-30 pointer-events-none': !agcEnabled }">
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs opacity-70">Target Level</span>
            <span class="text-xs font-mono font-medium tabular-nums">{{ agcLevel }}%</span>
          </div>
          <input
            type="range"
            v-model.number="agcLevel"
            min="0"
            max="100"
            class="range range-xs range-primary w-full"
            :aria-label="`AGC target level: ${agcLevel}%`"
            :disabled="!agcEnabled"
            @input="handleAGCLevelChange"
          />
        </div>
      </div>

      <div class="divider my-0 opacity-30"></div>

      <!-- Voice Activity Detection -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Voice Activity Detection</p>
            <p class="text-xs opacity-50 mt-0.5">Skip silent frames to save bandwidth</p>
          </div>
          <input
            type="checkbox"
            v-model="vadEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle voice activity detection"
            @change="handleVADToggle"
          />
        </div>
        <div class="mt-3 transition-opacity" :class="{ 'opacity-30 pointer-events-none': !vadEnabled }">
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs opacity-70">Sensitivity</span>
            <span class="text-xs font-mono font-medium tabular-nums">{{ vadThreshold }}%</span>
          </div>
          <input
            type="range"
            v-model.number="vadThreshold"
            min="0"
            max="100"
            class="range range-xs range-primary w-full"
            :aria-label="`VAD sensitivity: ${vadThreshold}%`"
            :disabled="!vadEnabled"
            @input="handleVADThresholdChange"
          />
        </div>
      </div>

    </div>
  </section>
</template>
