<script setup>
import { ref } from 'vue'
import StarRating from './StarRating.vue'
import { rateClip } from '../api.js'

const props = defineProps({
  clip: { type: Object, required: true },
})
const emit = defineEmits(['rated'])

// The Vidstack player is heavy — only instantiate it after the user
// clicks the thumbnail. Until then we show a static poster + play hint.
const playing = ref(false)

function fmtTime(sec) {
  const s = Math.floor(sec)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

async function onRate(n) {
  try {
    const updated = await rateClip(props.clip.id, n)
    emit('rated', updated)
  } catch (e) {
    console.error('rate failed', e)
  }
}
</script>

<template>
  <article class="card">
    <div class="thumb-wrap" @click="playing = true">
      <media-player
        v-if="playing"
        :src="clip.video_url"
        :title="clip.title"
        class="card-player"
        playsinline
        autoplay
      >
        <media-provider></media-provider>
        <media-video-layout></media-video-layout>
      </media-player>
      <template v-else>
        <img class="thumb-img" :src="clip.thumb_url" :alt="clip.title" loading="lazy" />
        <div class="thumb-hint">
          <div class="play-mark">▶</div>
        </div>
        <span v-if="clip.llm_rank" class="rank-badge">#{{ clip.llm_rank }}</span>
        <span class="dur-badge">{{ fmtTime(clip.duration) }}</span>
      </template>
    </div>

    <div class="body">
      <div class="meta-row">
        <span class="game-tag">{{ clip.game }}</span>
        <span>{{ clip.recorded_at.slice(0, 10) }}</span>
        <span class="dot">·</span>
        <span>@ {{ fmtTime(clip.start_s) }}</span>
        <span class="dot">·</span>
        <span>score {{ clip.score.toFixed(1) }}</span>
      </div>

      <h3 class="title">{{ clip.title }}</h3>

      <div class="action-row">
        <StarRating :model-value="clip.rating" @update:model-value="onRate" />
        <a
          class="dl-btn"
          :href="clip.download_url"
          :download="''"
          :title="`Download ${clip.title}`"
          aria-label="Download clip"
          @click.stop
        >
          <span class="dl-icon" aria-hidden="true">↓</span>
          <span class="dl-label">Download</span>
        </a>
      </div>

      <div v-if="clip.reason" class="section">
        <div class="section-label">Why it matters</div>
        <p class="section-body">{{ clip.reason }}</p>
      </div>

      <div v-if="clip.contents" class="section">
        <div class="section-label">What happens</div>
        <p class="section-body">{{ clip.contents }}</p>
      </div>

      <div v-if="clip.tags && clip.tags.length" class="tagbar">
        <span v-for="t in clip.tags" :key="t" class="tag">#{{ t }}</span>
      </div>

      <details class="transcript">
        <summary>Transcript</summary>
        <p>{{ clip.transcript }}</p>
      </details>
    </div>
  </article>
</template>
