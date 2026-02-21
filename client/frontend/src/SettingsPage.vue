<script setup lang="ts">
import { ref } from 'vue'
import AudioDeviceSettings from './AudioDeviceSettings.vue'
import VoiceProcessing from './VoiceProcessing.vue'
import KeybindsSettings from './KeybindsSettings.vue'
import AppearanceSettings from './AppearanceSettings.vue'
import AboutSettings from './AboutSettings.vue'

const emit = defineEmits<{
  back: []
}>()

type SettingsTab = 'audio' | 'appearance' | 'keybinds' | 'about'
const activeTab = ref<SettingsTab>('audio')

const tabs: { id: SettingsTab; label: string }[] = [
  { id: 'audio', label: 'Audio' },
  { id: 'appearance', label: 'Appearance' },
  { id: 'keybinds', label: 'Keybinds' },
  { id: 'about', label: 'About' },
]
</script>

<template>
  <section class="flex h-full min-h-0 flex-col bg-base-100">
    <header class="flex items-center gap-2 px-3 sm:px-5 py-3 border-b border-base-content/10 shrink-0">
      <button class="btn btn-ghost btn-sm" @click="emit('back')" aria-label="Back to room">
        ← Back
      </button>
      <h2 class="text-sm font-semibold">Settings</h2>
    </header>

    <!-- Tabs -->
    <div class="tabs tabs-bordered px-3 sm:px-5 pt-2 shrink-0" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="tab tab-sm"
        :class="{ 'tab-active': activeTab === tab.id }"
        role="tab"
        :aria-selected="activeTab === tab.id"
        @click="activeTab = tab.id"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- Tab content -->
    <div class="flex-1 min-h-0 overflow-y-auto">
      <div class="p-3 sm:p-5 flex flex-col gap-4 sm:gap-5 max-w-sm w-full">
        <template v-if="activeTab === 'audio'">
          <AudioDeviceSettings />
          <VoiceProcessing />
        </template>

        <template v-else-if="activeTab === 'appearance'">
          <AppearanceSettings />
        </template>

        <template v-else-if="activeTab === 'keybinds'">
          <KeybindsSettings />
        </template>

        <template v-else-if="activeTab === 'about'">
          <AboutSettings />
        </template>
      </div>
    </div>
  </section>
</template>
