<script setup lang="ts">
const props = defineProps<{
  user: { id: number; username: string }
  speaking: boolean
}>()

function initials(name: string) {
  return name.slice(0, 2).toUpperCase()
}
</script>

<template>
  <div class="flex flex-col items-center gap-2 select-none">
    <div
      class="w-16 h-16 rounded-full flex items-center justify-center text-lg font-bold bg-primary text-primary-content"
      :class="speaking ? 'speaking' : 'silent'"
    >
      {{ initials(user.username) }}
    </div>
    <span class="text-sm font-medium max-w-24 truncate">{{ user.username }}</span>
  </div>
</template>

<style scoped>
.silent, .speaking {
  transition: outline-color 0.15s ease, outline-width 0.15s ease;
  outline-style: solid;
  outline-offset: 3px;
}
.silent {
  outline-width: 3px;
  outline-color: transparent;
}
.speaking {
  outline-width: 3px;
  outline-color: var(--color-success);
}
</style>
