<script setup lang="ts">
defineProps<{
  settingsOpen: boolean
  muted: boolean
  deafened: boolean
}>()
const emit = defineEmits<{
  'settings-toggle': []
  'mute-toggle': []
  'deafen-toggle': []
  'server-browser': []
  disconnect: []
}>()
</script>

<template>
  <div class="flex flex-col items-center py-3 gap-1 border-r border-base-content/10 bg-base-300" style="width:52px;min-width:52px">

    <!-- Server Browser (first item) -->
    <button
      class="btn btn-ghost btn-sm btn-square tooltip tooltip-right opacity-60 hover:opacity-100 mb-2"
      data-tip="Server Browser"
      @click="emit('server-browser')"
    >
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z" />
      </svg>
    </button>

    <div class="flex-1" />

    <!-- Mute -->
    <button
      class="btn btn-ghost btn-sm btn-square tooltip tooltip-right"
      :data-tip="muted ? 'Unmute' : 'Mute'"
      :class="muted ? 'text-error opacity-100' : 'opacity-60 hover:opacity-100'"
      @click="emit('mute-toggle')"
    >
      <!-- Mic on -->
      <svg v-if="!muted" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
      </svg>
      <!-- Mic off (slashed) -->
      <svg v-else xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 18.75a6 6 0 006-6v-1.5m-6 7.5a6 6 0 01-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 01-3-3V4.5a3 3 0 116 0v8.25a3 3 0 01-3 3z" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18" />
      </svg>
    </button>

    <!-- Deafen -->
    <button
      class="btn btn-ghost btn-sm btn-square tooltip tooltip-right"
      :data-tip="deafened ? 'Undeafen' : 'Deafen'"
      :class="deafened ? 'text-error opacity-100' : 'opacity-60 hover:opacity-100'"
      @click="emit('deafen-toggle')"
    >
      <!-- Headphones on -->
      <svg v-if="!deafened" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M14.463 10.29a3.75 3.75 0 010 3.42M9.537 13.71a3.75 3.75 0 010-3.42M4.886 18.364a9 9 0 010-12.728M12 12h.008v.008H12V12z" />
      </svg>
      <!-- Headphones off (slashed) -->
      <svg v-else xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.114 5.636a9 9 0 010 12.728M14.463 10.29a3.75 3.75 0 010 3.42M9.537 13.71a3.75 3.75 0 010-3.42M4.886 18.364a9 9 0 010-12.728M12 12h.008v.008H12V12z" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18" />
      </svg>
    </button>

    <!-- Settings -->
    <button
      class="btn btn-ghost btn-sm btn-square tooltip tooltip-right"
      data-tip="Settings"
      :class="settingsOpen ? 'text-primary' : 'opacity-60 hover:opacity-100'"
      @click="emit('settings-toggle')"
    >
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 010 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 010-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28z" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      </svg>
    </button>

    <!-- Disconnect -->
    <button
      class="btn btn-ghost btn-sm btn-square tooltip tooltip-right opacity-60 hover:text-error hover:opacity-100 mb-1"
      data-tip="Disconnect"
      @click="emit('disconnect')"
    >
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9" />
      </svg>
    </button>
  </div>
</template>
