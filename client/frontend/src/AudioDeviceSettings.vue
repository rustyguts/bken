<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  GetInputDevices,
  GetOutputDevices,
  SetInputDevice,
  SetOutputDevice,
  SetVolume,
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
const testing = ref(false)
const testError = ref('')

async function persistConfig(): Promise<void> {
  const cfg = await GetConfig()
  await SaveConfig({
    ...cfg,
    input_device_id: selectedInput.value,
    output_device_id: selectedOutput.value,
    volume: volume.value / 100,
  })
}

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

onMounted(async () => {
  const [devices, cfg] = await Promise.all([
    GetInputDevices(),
    GetConfig(),
  ])
  inputDevices.value = devices || []
  outputDevices.value = (await GetOutputDevices()) || []

  if (cfg.input_device_id !== -1) selectedInput.value = cfg.input_device_id
  if (cfg.output_device_id !== -1) selectedOutput.value = cfg.output_device_id
  volume.value = Math.round(cfg.volume * 100)

  if (cfg.input_device_id !== -1) await SetInputDevice(cfg.input_device_id)
  if (cfg.output_device_id !== -1) await SetOutputDevice(cfg.output_device_id)
})
</script>

<template>
  <!-- Input -->
  <section>
    <div class="flex items-center gap-2 mb-3">
      <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
      </svg>
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Input</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4 flex flex-col gap-4">
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

  <!-- Output -->
  <section>
    <div class="flex items-center gap-2 mb-3">
      <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-primary shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M16.463 8.288a5.25 5.25 0 010 7.424M6.75 8.25l4.72-4.72a.75.75 0 011.28.53v15.88a.75.75 0 01-1.28.53l-4.72-4.72H4.51c-.88 0-1.704-.507-1.938-1.354A9.01 9.01 0 012.25 12c0-.83.112-1.633.322-2.396C2.806 8.756 3.63 8.25 4.51 8.25H6.75z" />
      </svg>
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">Output</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4 flex flex-col gap-4">
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
</template>
