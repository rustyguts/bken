<script setup lang="ts">
import AudioDeviceSettings from './AudioDeviceSettings.vue'
import VoiceProcessing from './VoiceProcessing.vue'
import ThemePicker from './ThemePicker.vue'

withDefaults(defineProps<{
  showHeader?: boolean
}>(), {
  showHeader: true,
})
</script>

<template>
  <div class="flex flex-col h-full overflow-y-auto">

    <!-- Page header -->
    <div v-if="showHeader" class="px-3 sm:px-5 py-3 border-b border-base-content/10 shrink-0">
      <h2 class="text-sm font-semibold">Settings</h2>
    </div>

    <div class="p-3 sm:p-5 flex flex-col gap-4 sm:gap-5 max-w-sm w-full">
      <AudioDeviceSettings />
      <VoiceProcessing />
      <ThemePicker />
    </div>
  </div>
</template>
