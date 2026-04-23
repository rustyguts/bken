<script setup>
defineProps({
  games:    { type: Array,  required: true },
  total:    { type: Number, required: true },
  selected: { type: String, default: null },
})
defineEmits(['select'])
</script>

<template>
  <aside class="sidebar">
    <h3 class="side-head">Games</h3>
    <nav class="game-list">
      <button
        class="game-item"
        :class="{ active: !selected }"
        @click="$emit('select', null)"
      >
        <span class="game-name">All games</span>
        <span class="game-count">{{ total }}</span>
      </button>
      <button
        v-for="g in games"
        :key="g.game"
        class="game-item"
        :class="{ active: g.game === selected }"
        @click="$emit('select', g.game)"
      >
        <span class="game-name">{{ g.game }}</span>
        <span class="game-count">{{ g.n }}</span>
      </button>
    </nav>
  </aside>
</template>
