<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import {
  SetNoiseSuppression,
  SetNoiseSuppressionLevel,
} from '../wailsjs/go/main/App'
import { GetConfig, SaveConfig, SetAEC, SetAGC, SetAGCLevel, SetVAD, SetVADThreshold, SetNoiseGate, SetNoiseGateThreshold, GetInputLevel, SetNotificationVolume, GetNotificationVolume } from './config'
import { SlidersHorizontal } from 'lucide-vue-next'

const aecEnabled = ref(true)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
const agcEnabled = ref(true)
const agcLevel = ref(50)
const vadEnabled = ref(true)
const vadThreshold = ref(30)
const gateEnabled = ref(false)
const gateThreshold = ref(20)
const inputLevel = ref(0)
const notifVolume = ref(100) // 0-200%
let levelTimer: ReturnType<typeof setInterval> | null = null

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
    noise_gate_enabled: gateEnabled.value,
    noise_gate_threshold: gateThreshold.value,
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

async function handleGateToggle(): Promise<void> {
  await SetNoiseGate(gateEnabled.value)
  await persistConfig()
}

async function handleGateThresholdChange(): Promise<void> {
  await SetNoiseGateThreshold(gateThreshold.value)
  await persistConfig()
}

async function handleNotifVolumeChange(): Promise<void> {
  await SetNotificationVolume(notifVolume.value / 100)
}

// Input level meter: polls ~15fps
function startLevelMeter(): void {
  if (levelTimer) return
  levelTimer = setInterval(async () => {
    try {
      inputLevel.value = await GetInputLevel()
    } catch {
      inputLevel.value = 0
    }
  }, 66) // ~15fps
}

function stopLevelMeter(): void {
  if (levelTimer) {
    clearInterval(levelTimer)
    levelTimer = null
  }
  inputLevel.value = 0
}

// Level meter color: green < 0.3, yellow 0.3-0.7, red > 0.7
const levelColor = computed(() => {
  const l = inputLevel.value
  if (l > 0.7) return 'bg-error'
  if (l > 0.3) return 'bg-warning'
  return 'bg-success'
})

// Level as percentage (capped at 100)
const levelPercent = computed(() => Math.min(100, Math.round(inputLevel.value * 100 / 0.5)))

onMounted(async () => {
  const cfg = await GetConfig()
  aecEnabled.value = cfg.aec_enabled
  noiseEnabled.value = cfg.noise_enabled
  noiseLevel.value = cfg.noise_level
  agcEnabled.value = cfg.agc_enabled
  agcLevel.value = cfg.agc_level
  vadEnabled.value = cfg.vad_enabled
  vadThreshold.value = cfg.vad_threshold
  gateEnabled.value = cfg.noise_gate_enabled ?? false
  gateThreshold.value = cfg.noise_gate_threshold ?? 20
  try {
    const vol = await GetNotificationVolume()
    notifVolume.value = Math.round(vol * 100)
  } catch {
    notifVolume.value = 100
  }
  startLevelMeter()
})

onBeforeUnmount(() => {
  stopLevelMeter()
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <SlidersHorizontal class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
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

      <div class="divider my-0 opacity-30"></div>

      <!-- Noise Gate -->
      <div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium leading-none">Noise Gate</p>
            <p class="text-xs opacity-50 mt-0.5">Zero out audio below threshold</p>
          </div>
          <input
            type="checkbox"
            v-model="gateEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle noise gate"
            @change="handleGateToggle"
          />
        </div>
        <div class="mt-3 transition-opacity" :class="{ 'opacity-30 pointer-events-none': !gateEnabled }">
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs opacity-70">Threshold</span>
            <span class="text-xs font-mono font-medium tabular-nums">{{ gateThreshold }}%</span>
          </div>
          <input
            type="range"
            v-model.number="gateThreshold"
            min="0"
            max="100"
            class="range range-xs range-primary w-full"
            :aria-label="`Noise gate threshold: ${gateThreshold}%`"
            :disabled="!gateEnabled"
            @input="handleGateThresholdChange"
          />
        </div>
      </div>

      <div class="divider my-0 opacity-30"></div>

      <!-- Notification Volume -->
      <div>
        <div class="flex items-center justify-between mb-2">
          <div>
            <p class="text-sm font-medium leading-none">Notification Volume</p>
            <p class="text-xs opacity-50 mt-0.5">Join/leave/mute sounds</p>
          </div>
          <span class="text-xs font-mono font-medium tabular-nums">{{ notifVolume }}%</span>
        </div>
        <input
          type="range"
          v-model.number="notifVolume"
          min="0"
          max="200"
          step="5"
          class="range range-xs range-primary w-full"
          :aria-label="`Notification volume: ${notifVolume}%`"
          @input="handleNotifVolumeChange"
        />
      </div>

      <div class="divider my-0 opacity-30"></div>

      <!-- Input Level Meter -->
      <div>
        <div class="flex items-center justify-between mb-2">
          <div>
            <p class="text-sm font-medium leading-none">Input Level</p>
            <p class="text-xs opacity-50 mt-0.5">Real-time mic input</p>
          </div>
          <span class="text-xs font-mono font-medium tabular-nums">{{ Math.round(inputLevel * 100) }}%</span>
        </div>
        <div class="w-full h-2 bg-base-300 rounded-full overflow-hidden">
          <div
            class="h-full rounded-full transition-all duration-75"
            :class="levelColor"
            :style="{ width: levelPercent + '%' }"
          ></div>
        </div>
      </div>

    </div>
  </section>
</template>
