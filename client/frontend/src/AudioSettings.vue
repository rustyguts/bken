<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  GetInputDevices,
  GetOutputDevices,
  SetInputDevice,
  SetOutputDevice,
  SetVolume,
  SetNoiseSuppression,
  SetNoiseSuppressionLevel,
  StartTest,
  StopTest,
} from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'
import { GetConfig, SaveConfig, SetAGC, SetAGCLevel, SetVAD, SetVADThreshold } from './config'

const inputDevices = ref<main.AudioDevice[]>([])
const outputDevices = ref<main.AudioDevice[]>([])
const selectedInput = ref(-1)
const selectedOutput = ref(-1)
const volume = ref(100)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
const agcEnabled = ref(true)
const agcLevel = ref(50)
const vadEnabled = ref(true)
const vadThreshold = ref(30)
const testing = ref(false)
const testError = ref('')

const THEMES = [
  // Light
  { name: 'light', label: 'Light' },
  { name: 'cupcake', label: 'Cupcake' },
  { name: 'bumblebee', label: 'Bumblebee' },
  { name: 'emerald', label: 'Emerald' },
  { name: 'corporate', label: 'Corporate' },
  { name: 'retro', label: 'Retro' },
  { name: 'cyberpunk', label: 'Cyberpunk' },
  { name: 'valentine', label: 'Valentine' },
  { name: 'garden', label: 'Garden' },
  { name: 'lofi', label: 'Lo-Fi' },
  { name: 'pastel', label: 'Pastel' },
  { name: 'fantasy', label: 'Fantasy' },
  { name: 'wireframe', label: 'Wireframe' },
  { name: 'cmyk', label: 'CMYK' },
  { name: 'autumn', label: 'Autumn' },
  { name: 'acid', label: 'Acid' },
  { name: 'lemonade', label: 'Lemonade' },
  { name: 'winter', label: 'Winter' },
  { name: 'nord', label: 'Nord' },
  { name: 'caramellatte', label: 'Caramel Latte' },
  { name: 'silk', label: 'Silk' },
  // Dark
  { name: 'dark', label: 'Dark' },
  { name: 'synthwave', label: 'Synthwave' },
  { name: 'halloween', label: 'Halloween' },
  { name: 'forest', label: 'Forest' },
  { name: 'aqua', label: 'Aqua' },
  { name: 'black', label: 'Black' },
  { name: 'luxury', label: 'Luxury' },
  { name: 'dracula', label: 'Dracula' },
  { name: 'business', label: 'Business' },
  { name: 'night', label: 'Night' },
  { name: 'coffee', label: 'Coffee' },
  { name: 'dim', label: 'Dim' },
  { name: 'sunset', label: 'Sunset' },
  { name: 'abyss', label: 'Abyss' },
] as const

const currentTheme = ref('dark')

async function persistConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    theme: currentTheme.value,
    input_device_id: selectedInput.value,
    output_device_id: selectedOutput.value,
    volume: volume.value / 100,
    noise_enabled: noiseEnabled.value,
    noise_level: noiseLevel.value,
    agc_enabled: agcEnabled.value,
    agc_level: agcLevel.value,
    vad_enabled: vadEnabled.value,
    vad_threshold: vadThreshold.value,
  })
}

async function applyTheme(theme: string): Promise<void> {
  currentTheme.value = theme
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem('bken-theme', theme)
  await persistConfig()
}

onMounted(async () => {
  const [devices, cfg] = await Promise.all([
    GetInputDevices(),
    GetConfig(),
  ])
  inputDevices.value = devices || []
  outputDevices.value = (await GetOutputDevices()) || []

  // Restore settings from config
  if (cfg.input_device_id !== -1) selectedInput.value = cfg.input_device_id
  if (cfg.output_device_id !== -1) selectedOutput.value = cfg.output_device_id
  volume.value = Math.round(cfg.volume * 100)
  noiseEnabled.value = cfg.noise_enabled
  noiseLevel.value = cfg.noise_level
  agcEnabled.value = cfg.agc_enabled
  agcLevel.value = cfg.agc_level
  vadEnabled.value = cfg.vad_enabled
  vadThreshold.value = cfg.vad_threshold

  // Theme: config is source of truth; keep localStorage in sync for fast startup
  const validTheme = THEMES.some(t => t.name === cfg.theme)
  if (validTheme) {
    currentTheme.value = cfg.theme
    document.documentElement.setAttribute('data-theme', cfg.theme)
    localStorage.setItem('bken-theme', cfg.theme)
  }

  // Note: audio settings are applied on app startup via ApplyConfig() in App.vue.
  // Here we only need to apply device selection since PortAudio devices are
  // re-enumerated lazily and need to be set before the next Connect call.
  if (cfg.input_device_id !== -1) await SetInputDevice(cfg.input_device_id)
  if (cfg.output_device_id !== -1) await SetOutputDevice(cfg.output_device_id)
})

async function handleInputChange(): Promise<void> {
  await SetInputDevice(selectedInput.value)
  await persistConfig()
}

async function handleOutputChange(): Promise<void> {
  await SetOutputDevice(selectedOutput.value)
  await persistConfig()
}

async function handleVolumeChange(): Promise<void> {
  await SetVolume(volume.value / 100)
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

async function toggleTest(): Promise<void> {
  if (testing.value) {
    await StopTest()
    testing.value = false
    testError.value = ''
  } else {
    const err = await StartTest()
    if (err) {
      testError.value = err
    } else {
      testing.value = true
      testError.value = ''
    }
  }
}
</script>

<template>
  <div class="flex flex-col h-full overflow-y-auto">

    <!-- Page header -->
    <div class="px-5 py-3 border-b border-base-content/10 shrink-0">
      <h2 class="text-sm font-semibold">Settings</h2>
    </div>

    <div class="p-5 flex flex-col gap-5 max-w-sm">

      <!-- ── Input ─────────────────────────────────────── -->
      <section>
        <div class="flex items-center gap-2 mb-3">
          <!-- Microphone icon -->
          <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
          </svg>
          <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Input</span>
        </div>

        <div class="rounded-xl border border-base-content/10 bg-base-200/40 p-4 flex flex-col gap-4">
          <label class="form-control w-full">
            <div class="label pb-1 pt-0"><span class="label-text text-xs opacity-70">Microphone</span></div>
            <select
              v-model.number="selectedInput"
              class="select select-bordered select-sm w-full"
              aria-label="Microphone device"
              @change="handleInputChange"
            >
              <option :value="-1">Default</option>
              <option v-for="dev in inputDevices" :key="dev.id" :value="dev.id">{{ dev.name }}</option>
            </select>
          </label>

          <div class="flex gap-2 items-end">
            <button
              class="btn btn-sm flex-1 transition-all"
              :class="testing ? 'btn-info' : 'btn-outline'"
              :aria-label="testing ? 'Stop microphone test' : 'Test microphone loopback'"
              @click="toggleTest"
            >
              <svg v-if="!testing" xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.348a1.125 1.125 0 010 1.971l-11.54 6.347a1.125 1.125 0 01-1.667-.985V5.653z" />
              </svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 7.5A2.25 2.25 0 017.5 5.25h9a2.25 2.25 0 012.25 2.25v9a2.25 2.25 0 01-2.25 2.25h-9a2.25 2.25 0 01-2.25-2.25v-9z" />
              </svg>
              {{ testing ? 'Stop Test' : 'Test Mic' }}
            </button>
          </div>

          <div v-if="testError" role="alert" class="alert alert-error text-xs py-1.5">
            {{ testError }}
          </div>
        </div>
      </section>

      <!-- ── Output ─────────────────────────────────────── -->
      <section>
        <div class="flex items-center gap-2 mb-3">
          <!-- Speaker icon -->
          <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
            <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M16.463 8.288a5.25 5.25 0 010 7.424M6.75 8.25l4.72-4.72a.75.75 0 011.28.53v15.88a.75.75 0 01-1.28.53l-4.72-4.72H4.51c-.88 0-1.704-.507-1.938-1.354A9.01 9.01 0 012.25 12c0-.83.112-1.633.322-2.396C2.806 8.756 3.63 8.25 4.51 8.25H6.75z" />
          </svg>
          <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Output</span>
        </div>

        <div class="rounded-xl border border-base-content/10 bg-base-200/40 p-4 flex flex-col gap-4">
          <label class="form-control w-full">
            <div class="label pb-1 pt-0"><span class="label-text text-xs opacity-70">Speaker</span></div>
            <select
              v-model.number="selectedOutput"
              class="select select-bordered select-sm w-full"
              aria-label="Speaker device"
              @change="handleOutputChange"
            >
              <option :value="-1">Default</option>
              <option v-for="dev in outputDevices" :key="dev.id" :value="dev.id">{{ dev.name }}</option>
            </select>
          </label>

          <div>
            <div class="flex items-center justify-between mb-2">
              <span class="text-xs opacity-70">Volume</span>
              <span class="text-xs font-mono font-medium tabular-nums">{{ volume }}%</span>
            </div>
            <input
              type="range"
              v-model.number="volume"
              min="0"
              max="100"
              class="range range-sm range-primary w-full"
              aria-label="Playback volume"
              @input="handleVolumeChange"
            />
          </div>
        </div>
      </section>

      <!-- ── Voice Processing ───────────────────────────── -->
      <section>
        <div class="flex items-center gap-2 mb-3">
          <!-- Adjustments / sliders icon -->
          <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
            <path stroke-linecap="round" stroke-linejoin="round" d="M10.5 6h9.75M10.5 6a1.5 1.5 0 11-3 0m3 0a1.5 1.5 0 10-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 01-3 0m3 0a1.5 1.5 0 00-3 0m-9.75 0h9.75" />
          </svg>
          <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Voice Processing</span>
        </div>

        <div class="rounded-xl border border-base-content/10 bg-base-200/40 p-4 flex flex-col gap-4">

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

      <!-- ── Appearance ─────────────────────────────────── -->
      <section>
        <div class="flex items-center gap-2 mb-3">
          <!-- Swatch / palette icon -->
          <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4.098 19.902a3.75 3.75 0 005.304 0l6.401-6.402M6.75 21A3.75 3.75 0 013 17.25V4.125C3 3.504 3.504 3 4.125 3h5.25c.621 0 1.125.504 1.125 1.125v4.072M6.75 21a3.75 3.75 0 003.75-3.75V8.197M6.75 21h13.125c.621 0 1.125-.504 1.125-1.125v-5.25c0-.621-.504-1.125-1.125-1.125h-4.072M10.5 8.197l2.88-2.88c.438-.439 1.15-.439 1.59 0l3.712 3.713c.44.44.44 1.152 0 1.59l-2.879 2.88M6.75 17.25h.008v.008H6.75v-.008z" />
          </svg>
          <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Appearance</span>
        </div>

        <div class="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Theme selection">
          <button
            v-for="theme in THEMES"
            :key="theme.name"
            class="flex items-center gap-2 rounded-xl px-3 py-2 text-left text-xs transition-all cursor-pointer border"
            :class="currentTheme === theme.name
              ? 'border-primary bg-primary/10 shadow-sm'
              : 'border-base-content/10 hover:border-primary/40 hover:bg-base-200/60'"
            role="radio"
            :aria-checked="currentTheme === theme.name"
            :aria-label="`${theme.label} theme`"
            @click="applyTheme(theme.name)"
          >
            <div :data-theme="theme.name" class="flex gap-0.5 shrink-0">
              <span class="w-2 h-4 rounded-l-full bg-primary"></span>
              <span class="w-2 h-4 bg-secondary"></span>
              <span class="w-2 h-4 bg-accent"></span>
              <span class="w-2 h-4 rounded-r-full bg-neutral"></span>
            </div>
            <span class="truncate">{{ theme.label }}</span>
          </button>
        </div>
      </section>

    </div>
  </div>
</template>
