<script setup lang="ts">
import { ref, type Component } from 'vue'
import { AudioLines, Palette, Keyboard, CircleHelp, ChevronLeft } from 'lucide-vue-next'
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

const tabs: { id: SettingsTab; label: string; icon: Component }[] = [
  { id: 'audio', label: 'Audio', icon: AudioLines },
  { id: 'appearance', label: 'Appearance', icon: Palette },
  { id: 'keybinds', label: 'Keybinds', icon: Keyboard },
  { id: 'about', label: 'About', icon: CircleHelp },
]
</script>

<template>
  <section class="h-full min-h-0 bg-base-200/30">
    <div class="h-full min-h-0 p-3 sm:p-4 lg:p-6">
      <div class="card h-full min-h-0 border border-base-content/10 bg-base-100 shadow-xl">
        <div class="card-body h-full min-h-0 p-0">
          <header class="navbar min-h-0 border-b border-base-content/10 px-4 py-3 sm:px-5">
            <div class="flex-1 flex items-center gap-2">
              <button class="btn btn-sm btn-ghost" @click="emit('back')" aria-label="Back to channel">
                <ChevronLeft class="size-4" aria-hidden="true" />
                Back
              </button>
              <div class="divider divider-horizontal mx-0 h-5"></div>
              <div>
                <p class="text-xs uppercase tracking-wider opacity-60">Client</p>
                <h2 class="text-base font-semibold leading-none">Settings</h2>
              </div>
            </div>
          </header>

          <div class="flex-1 min-h-0 grid grid-rows-[auto_1fr] lg:grid-rows-1 lg:grid-cols-[260px_1fr]">
            <aside class="border-b border-base-content/10 lg:border-b-0 lg:border-r lg:border-base-content/10 p-3 sm:p-4">
              <div class="tabs tabs-box flex-col lg:flex-col" role="tablist" aria-label="Settings sections">
                <button
                  v-for="tab in tabs"
                  :key="tab.id"
                  role="tab"
                  :aria-selected="activeTab === tab.id"
                  class="tab gap-2 justify-start"
                  :class="activeTab === tab.id ? 'tab-active' : ''"
                  @click="activeTab = tab.id"
                >
                  <component :is="tab.icon" class="size-4 shrink-0" aria-hidden="true" />
                  {{ tab.label }}
                </button>
              </div>
            </aside>

            <div class="min-h-0 overflow-y-auto p-3 sm:p-4 lg:p-6">
              <div class="mx-auto w-full max-w-4xl space-y-5">
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

                <template v-else>
                  <AboutSettings />
                </template>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
