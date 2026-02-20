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
import { GetConfig, SaveConfig } from './config'

const inputDevices = ref<main.AudioDevice[]>([])
const outputDevices = ref<main.AudioDevice[]>([])
const selectedInput = ref(-1)
const selectedOutput = ref(-1)
const volume = ref(100)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
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

  // Theme: config is source of truth; keep localStorage in sync for fast startup
  const validTheme = THEMES.some(t => t.name === cfg.theme)
  if (validTheme) {
    currentTheme.value = cfg.theme
    document.documentElement.setAttribute('data-theme', cfg.theme)
    localStorage.setItem('bken-theme', cfg.theme)
  }

  // Apply saved audio settings
  if (cfg.input_device_id !== -1) await SetInputDevice(cfg.input_device_id)
  if (cfg.output_device_id !== -1) await SetOutputDevice(cfg.output_device_id)
  await SetVolume(cfg.volume)
  await SetNoiseSuppression(cfg.noise_enabled)
  await SetNoiseSuppressionLevel(cfg.noise_level)
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
    <div class="px-4 py-2 text-xs font-semibold uppercase tracking-wider opacity-40 border-b border-base-content/10 shrink-0">
      Audio Settings
    </div>
    <div class="p-6 flex flex-col gap-4 max-w-sm">
      <label class="form-control w-full">
        <div class="label"><span class="label-text text-xs">Microphone</span></div>
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

      <label class="form-control w-full">
        <div class="label"><span class="label-text text-xs">Speaker</span></div>
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

      <label class="form-control w-full">
        <div class="label"><span class="label-text text-xs">Volume: {{ volume }}%</span></div>
        <input
          type="range"
          v-model.number="volume"
          min="0"
          max="100"
          class="range range-sm range-primary"
          aria-label="Playback volume"
          @input="handleVolumeChange"
        />
      </label>

      <label class="form-control w-full">
        <div class="label cursor-pointer">
          <span class="label-text text-xs">Noise Suppression</span>
          <input
            type="checkbox"
            v-model="noiseEnabled"
            class="toggle toggle-primary toggle-sm"
            aria-label="Toggle noise suppression"
            @change="handleNoiseToggle"
          />
        </div>
      </label>

      <label class="form-control w-full" :class="{ 'opacity-40 pointer-events-none': !noiseEnabled }">
        <div class="label"><span class="label-text text-xs">Level: {{ noiseLevel }}%</span></div>
        <input
          type="range"
          v-model.number="noiseLevel"
          min="0"
          max="100"
          class="range range-sm range-primary"
          :aria-label="`Noise suppression level: ${noiseLevel}%`"
          :disabled="!noiseEnabled"
          @input="handleNoiseLevelChange"
        />
      </label>

      <button
        class="btn btn-outline btn-sm w-full"
        :class="{ 'btn-info': testing }"
        :aria-label="testing ? 'Stop microphone test' : 'Start microphone test'"
        @click="toggleTest"
      >
        {{ testing ? 'Stop Test' : 'Test Mic' }}
      </button>

      <div v-if="testError" role="alert" class="alert alert-error text-xs py-1">
        {{ testError }}
      </div>
    </div>

    <div class="px-4 py-2 text-xs font-semibold uppercase tracking-wider opacity-40 border-t border-b border-base-content/10 shrink-0">
      Appearance
    </div>
    <div class="p-4">
      <div class="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Theme selection">
        <button
          v-for="theme in THEMES"
          :key="theme.name"
          class="flex items-center gap-2 rounded-lg px-3 py-2 text-left text-xs transition-colors cursor-pointer border"
          :class="currentTheme === theme.name
            ? 'border-primary bg-primary/10'
            : 'border-base-content/10 hover:border-base-content/30'"
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
    </div>
  </div>
</template>
