<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { fetchClips, fetchGames } from './api.js'
import Sidebar from './components/Sidebar.vue'
import Toolbar from './components/Toolbar.vue'
import ClipCard from './components/ClipCard.vue'

// Filter state. Defaults match the Python DEFAULT_SORT so the first
// request doesn't re-fetch just because the UI picked something different.
const game = ref(null)
const sort = ref('llm_rank')
const minRating = ref(0)

const clips = ref([])
const games = ref([])
const sortOptions = ref([])
const loading = ref(false)
const error = ref(null)

async function reload() {
  loading.value = true
  error.value = null
  try {
    const [c, g] = await Promise.all([
      fetchClips({ game: game.value, sort: sort.value, min_rating: minRating.value }),
      fetchGames(),
    ])
    clips.value = c.clips
    sortOptions.value = c.sort_options
    games.value = g.games
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(reload)
watch([game, sort, minRating], reload)

function onRated(updated) {
  // Replace the updated clip in place — avoids a full re-fetch for one change.
  // But if the current sort is by rating, we do refresh so the position
  // updates.
  const i = clips.value.findIndex((c) => c.id === updated.id)
  if (i !== -1) clips.value[i] = updated
  if (sort.value.startsWith('rating_')) reload()
}

const total = computed(() => games.value.reduce((s, g) => s + g.n, 0))
</script>

<template>
  <header class="topbar">
    <div class="brand">
      <span class="brand-mark">▮▮▮</span>
      <span class="brand-name">vidpipe</span>
      <span class="brand-sub">clip archive</span>
    </div>
    <div class="topbar-spacer"></div>
    <div class="stat">
      <span class="stat-num">{{ total }}</span>
      <span class="stat-label">clips</span>
    </div>
    <div class="stat">
      <span class="stat-num">{{ games.length }}</span>
      <span class="stat-label">games</span>
    </div>
  </header>

  <div class="layout">
    <Sidebar
      :games="games"
      :total="total"
      :selected="game"
      @select="game = $event"
    />

    <main class="main">
      <Toolbar
        v-model:sort="sort"
        v-model:min-rating="minRating"
        :selected-game="game"
        :sort-options="sortOptions"
        @clear-game="game = null"
      />

      <div v-if="error" class="empty">
        <div class="empty-mark">!</div>
        <p>API error: {{ error }}</p>
      </div>

      <div v-else-if="!loading && clips.length === 0" class="empty">
        <div class="empty-mark">⌀</div>
        <p>No clips match the current filter.</p>
      </div>

      <div v-else class="grid">
        <ClipCard
          v-for="c in clips"
          :key="c.id"
          :clip="c"
          @rated="onRated"
        />
      </div>
    </main>
  </div>
</template>
