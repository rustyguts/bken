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
import { Mic, Volume2, Play, Square } from 'lucide-vue-next'

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
      <Mic class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
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
          <Play v-if="!testing" class="w-3.5 h-3.5" aria-hidden="true" />
          <Square v-else class="w-3.5 h-3.5" aria-hidden="true" />
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
      <Volume2 class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
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
