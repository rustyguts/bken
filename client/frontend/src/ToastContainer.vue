<script setup lang="ts">
import { useToast } from './composables/useToast'
import type { ToastType } from './composables/useToast'
import { X } from 'lucide-vue-next'

const { toasts, dismissToast } = useToast()

function alertClass(type: ToastType): string {
  switch (type) {
    case 'error': return 'alert-error'
    case 'warning': return 'alert-warning'
    case 'info': return 'alert-info'
    case 'success': return 'alert-success'
  }
}
</script>

<template>
  <div class="toast toast-top toast-end z-[100] mt-10">
    <TransitionGroup
      enter-active-class="transition-all duration-200 ease-out"
      enter-from-class="translate-x-full opacity-0"
      leave-active-class="transition-all duration-200 ease-in"
      leave-to-class="translate-x-full opacity-0"
    >
      <div
        v-for="toast in toasts"
        :key="toast.id"
        role="alert"
        class="alert max-w-sm shadow-lg text-sm"
        :class="alertClass(toast.type)"
      >
        <span class="flex-1">{{ toast.message }}</span>
        <button
          class="btn btn-ghost btn-xs btn-square"
          aria-label="Dismiss"
          @click="dismissToast(toast.id)"
        >
          <X class="w-3.5 h-3.5" aria-hidden="true" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
