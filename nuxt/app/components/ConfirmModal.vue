<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    title: string
    description?: string
    confirmLabel?: string
    cancelLabel?: string
    color?: 'primary' | 'error' | 'neutral' | 'warning'
  }>(),
  {
    confirmLabel: 'Confirm',
    cancelLabel: 'Cancel',
    color: 'primary',
  },
)

const emit = defineEmits<{ close: [boolean] }>()
const open = ref(true)

function confirm() {
  emit('close', true)
}
function cancel() {
  emit('close', false)
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    :description="description"
    :dismissible="true"
    @after:leave="emit('close', false)"
  >
    <template #footer>
      <div class="flex items-center justify-end gap-2 w-full">
        <UButton color="neutral" variant="ghost" :label="cancelLabel" @click="cancel" />
        <UButton :color="props.color" :label="confirmLabel" @click="confirm" />
      </div>
    </template>
  </UModal>
</template>
