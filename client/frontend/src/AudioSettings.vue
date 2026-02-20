<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { GetInputDevices, GetOutputDevices, SetInputDevice, SetOutputDevice, SetVolume, SetNoiseSuppression, SetNoiseSuppressionLevel, StartTest, StopTest } from '../wailsjs/go/main/App'

interface AudioDevice {
  id: number
  name: string
}

const inputDevices = ref<AudioDevice[]>([])
const outputDevices = ref<AudioDevice[]>([])
const selectedInput = ref(-1)
const selectedOutput = ref(-1)
const volume = ref(100)
const noiseEnabled = ref(false)
const noiseLevel = ref(80)
const testing = ref(false)
const testError = ref('')

onMounted(async () => {
  inputDevices.value = (await GetInputDevices()) || []
  outputDevices.value = (await GetOutputDevices()) || []
})

async function handleInputChange() {
  await SetInputDevice(selectedInput.value)
}

async function handleOutputChange() {
  await SetOutputDevice(selectedOutput.value)
}

async function handleVolumeChange() {
  await SetVolume(volume.value / 100)
}

async function handleNoiseToggle() {
  await SetNoiseSuppression(noiseEnabled.value)
}

async function handleNoiseLevelChange() {
  await SetNoiseSuppressionLevel(noiseLevel.value)
}

async function toggleTest() {
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
        <select v-model.number="selectedInput" class="select select-bordered select-sm w-full" @change="handleInputChange">
          <option :value="-1">Default</option>
          <option v-for="dev in inputDevices" :key="dev.id" :value="dev.id">{{ dev.name }}</option>
        </select>
      </label>

      <label class="form-control w-full">
        <div class="label"><span class="label-text text-xs">Speaker</span></div>
        <select v-model.number="selectedOutput" class="select select-bordered select-sm w-full" @change="handleOutputChange">
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
          @input="handleVolumeChange"
        />
      </label>

      <label class="form-control w-full">
        <div class="label cursor-pointer">
          <span class="label-text text-xs">Noise Suppression</span>
          <input type="checkbox" v-model="noiseEnabled" class="toggle toggle-primary toggle-sm"
                 @change="handleNoiseToggle" />
        </div>
      </label>

      <label class="form-control w-full" :class="{ 'opacity-40 pointer-events-none': !noiseEnabled }">
        <div class="label"><span class="label-text text-xs">Level: {{ noiseLevel }}%</span></div>
        <input type="range" v-model.number="noiseLevel" min="0" max="100"
               class="range range-sm range-primary" @input="handleNoiseLevelChange" />
      </label>

      <button
        class="btn btn-outline btn-sm w-full"
        :class="{ 'btn-info': testing }"
        @click="toggleTest"
      >
        {{ testing ? 'Stop Test' : 'Test Mic' }}
      </button>

      <div v-if="testError" role="alert" class="alert alert-error text-xs py-1">
        {{ testError }}
      </div>
    </div>
  </div>
</template>
