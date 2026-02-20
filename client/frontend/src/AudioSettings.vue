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

const inputDevices = ref<main.AudioDevice[]>([])
const outputDevices = ref<main.AudioDevice[]>([])
const selectedInput = ref(-1)
const selectedOutput = ref(-1)
const volume = ref(100)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
const testing = ref(false)
const testError = ref('')

const THEMES = ['light', 'dark', 'sunset'] as const
type Theme = typeof THEMES[number]
const THEME_LABELS: Record<Theme, string> = { light: 'Light', dark: 'Dark', sunset: 'Sunset' }
const currentTheme = ref<Theme>('dark')

function applyTheme(theme: Theme): void {
  currentTheme.value = theme
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem('bken-theme', theme)
}

onMounted(async () => {
  inputDevices.value = (await GetInputDevices()) || []
  outputDevices.value = (await GetOutputDevices()) || []
  const saved = localStorage.getItem('bken-theme') as Theme | null
  if (saved && (THEMES as readonly string[]).includes(saved)) {
    currentTheme.value = saved
  }
})

async function handleInputChange(): Promise<void> {
  await SetInputDevice(selectedInput.value)
}

async function handleOutputChange(): Promise<void> {
  await SetOutputDevice(selectedOutput.value)
}

async function handleVolumeChange(): Promise<void> {
  await SetVolume(volume.value / 100)
}

async function handleNoiseToggle(): Promise<void> {
  await SetNoiseSuppression(noiseEnabled.value)
}

async function handleNoiseLevelChange(): Promise<void> {
  await SetNoiseSuppressionLevel(noiseLevel.value)
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
    <div class="p-6 flex flex-col gap-4 max-w-sm">
      <fieldset class="form-control w-full">
        <legend class="label"><span class="label-text text-xs">Theme</span></legend>
        <div class="flex gap-2" role="radiogroup" aria-label="Theme selection">
          <button
            v-for="theme in THEMES"
            :key="theme"
            class="btn btn-sm flex-1"
            :class="currentTheme === theme ? 'btn-primary' : 'btn-ghost border border-base-content/20'"
            role="radio"
            :aria-checked="currentTheme === theme"
            :aria-label="`${THEME_LABELS[theme]} theme`"
            @click="applyTheme(theme)"
          >
            {{ THEME_LABELS[theme] }}
          </button>
        </div>
      </fieldset>
    </div>
  </div>
</template>
