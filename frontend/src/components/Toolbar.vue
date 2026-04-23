<script setup>
defineProps({
  sort:         { type: String, required: true },
  minRating:    { type: Number, required: true },
  selectedGame: { type: String, default: null },
  sortOptions:  { type: Array,  required: true },
})
defineEmits(['update:sort', 'update:minRating', 'clearGame'])
</script>

<template>
  <div class="toolbar">
    <div class="filter-pill-group">
      <label class="pill-label">Sort</label>
      <select
        class="pill-select"
        :value="sort"
        @change="$emit('update:sort', $event.target.value)"
      >
        <option v-for="o in sortOptions" :key="o.key" :value="o.key">
          {{ o.label }}
        </option>
      </select>
    </div>

    <div class="filter-pill-group">
      <label class="pill-label">Min rating</label>
      <div class="star-filter">
        <button
          v-for="n in [0, 1, 2, 3, 4, 5]"
          :key="n"
          class="starf"
          :class="{ active: n === minRating }"
          @click="$emit('update:minRating', n)"
        >
          <span v-if="n === 0">—</span>
          <span v-else>{{ '★'.repeat(n) }}</span>
        </button>
      </div>
    </div>

    <div v-if="selectedGame" class="filter-pill-group">
      <span class="pill-label">Game</span>
      <span class="pill-chip">
        {{ selectedGame }}
        <button class="chip-x" @click="$emit('clearGame')">✕</button>
      </span>
    </div>
  </div>
</template>
