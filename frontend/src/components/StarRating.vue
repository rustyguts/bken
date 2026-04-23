<script setup>
import { computed } from 'vue'

const props = defineProps({
  modelValue: { type: [Number, null], default: null },
})
const emit = defineEmits(['update:modelValue'])

const value = computed(() => props.modelValue ?? 0)

function click(n) {
  // Click the currently-lit star to clear; otherwise set to that star.
  emit('update:modelValue', value.value === n ? 0 : n)
}
</script>

<template>
  <div class="stars">
    <button
      v-for="n in [1, 2, 3, 4, 5]"
      :key="n"
      class="star"
      :class="{ on: value >= n }"
      :aria-label="`${n} star${n > 1 ? 's' : ''}`"
      @click="click(n)"
    >★</button>
    <span class="rating-value">
      {{ value ? `${value}/5` : 'unrated' }}
    </span>
  </div>
</template>
