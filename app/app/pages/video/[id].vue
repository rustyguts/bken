<script setup lang="ts">
import { SORT_OPTIONS } from '~/composables/useVideoEditor'
import type { Moment } from '~/composables/useVideoEditor'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const overlay = useOverlay()
const videoId = Number(route.params.id)

const {
  video,
  sortedMoments,
  loading,
  error,
  currentTime,
  selectedMoment,
  selectedMomentId,
  reload,
  updateMoment,
  deleteMoment,
  createClip,
  createMoment,
  selectMoment,
  sortField,
  sortDirection,
} = useVideoEditor(videoId)

const playerRef = ref<HTMLVideoElement | null>(null)
const momentListRef = ref<HTMLElement | null>(null)

const isEditingTitle = ref(false)
const titleDraft = ref('')

const clippingIds = ref(new Set<number>())

const viewMode = ref<'small' | 'detailed'>('small')

// Manual moment creation state
const manualStart = ref<number | null>(null)
const manualEnd = ref<number | null>(null)
const manualTitle = ref('')

onMounted(reload)

function fmtTime(sec: number): string {
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
  return `${m}:${String(ss).padStart(2, '0')}`
}

function onPlayerTimeUpdate() {
  if (playerRef.value) currentTime.value = playerRef.value.currentTime
}

function onSeek(time: number) {
  if (!playerRef.value) return
  playerRef.value.currentTime = time
  currentTime.value = time
  playerRef.value.play().catch(() => {})
}

function setStartToPlayhead() {
  if (!selectedMoment.value) return
  updateMoment(selectedMoment.value.id, { start_s: currentTime.value })
}

function setEndToPlayhead() {
  if (!selectedMoment.value) return
  updateMoment(selectedMoment.value.id, { end_s: currentTime.value })
}

function nudgeStart(delta: number) {
  if (!selectedMoment.value) return
  const v = Math.max(0, selectedMoment.value.start_s + delta)
  updateMoment(selectedMoment.value.id, { start_s: v })
}

function nudgeEnd(delta: number) {
  if (!selectedMoment.value) return
  updateMoment(selectedMoment.value.id, { end_s: selectedMoment.value.end_s + delta })
}

function beginEditTitle(m: { id: number; title: string | null }) {
  selectedMomentId.value = m.id
  titleDraft.value = m.title || ''
  isEditingTitle.value = true
}

async function saveTitle() {
  if (!selectedMoment.value) return
  await updateMoment(selectedMoment.value.id, { title: titleDraft.value.trim() })
  isEditingTitle.value = false
}

function cancelEditTitle() {
  isEditingTitle.value = false
  titleDraft.value = ''
}

function markIn() {
  manualStart.value = currentTime.value
  toast.add({ title: `In: ${fmtTime(currentTime.value)}`, color: 'primary', icon: 'i-lucide-flag' })
}

function markOut() {
  manualEnd.value = currentTime.value
  toast.add({ title: `Out: ${fmtTime(currentTime.value)}`, color: 'primary', icon: 'i-lucide-flag-off' })
}

async function onCreateManualMoment() {
  const start = manualStart.value ?? 0
  const end = manualEnd.value ?? start + 10
  if (end <= start) {
    toast.add({
      title: 'Invalid range',
      description: 'Out time must be after In time.',
      color: 'error',
      icon: 'i-lucide-triangle-alert',
    })
    return
  }
  await createMoment(start, end, manualTitle.value.trim() || 'Manual moment')
  manualStart.value = null
  manualEnd.value = null
  manualTitle.value = ''
  toast.add({ title: 'Moment created', color: 'success', icon: 'i-lucide-check' })
}

async function onDeleteMoment(id: number, title: string | null) {
  const ok = await confirmDialog({
    title: 'Delete moment?',
    description: title ? `"${title}" will be removed permanently.` : 'This moment will be removed permanently.',
    confirmLabel: 'Delete',
    color: 'error',
  })
  if (!ok) return
  await deleteMoment(id)
  toast.add({ title: 'Moment deleted', color: 'neutral', icon: 'i-lucide-trash-2' })
}

async function onCreateClip(id: number) {
  clippingIds.value.add(id)
  try {
    await createClip(id)
    toast.add({ title: 'Clip created', color: 'success', icon: 'i-lucide-film' })
  } finally {
    clippingIds.value.delete(id)
  }
}

async function onShareClip(id: number) {
  const url = `${window.location.origin}/share/${id}`
  try {
    await navigator.clipboard.writeText(url)
    toast.add({
      title: 'Share link copied',
      description: url,
      color: 'success',
      icon: 'i-lucide-link',
    })
  } catch {
    window.open(url, '_blank', 'noopener')
  }
}

async function onReprocess() {
  const ok = await confirmDialog({
    title: 'Reprocess this video?',
    description: 'Auto-generated moments may change; manual moments are preserved.',
    confirmLabel: 'Reprocess',
    color: 'primary',
  })
  if (!ok) return
  await $fetch(`/api/videos/${videoId}/reprocess`, {
    method: 'POST',
    body: { from_stage: 'score' },
  })
  toast.add({ title: 'Reprocess queued', color: 'primary', icon: 'i-lucide-refresh-cw' })
}

interface ConfirmOpts {
  title: string
  description?: string
  confirmLabel?: string
  color?: 'primary' | 'error' | 'neutral' | 'warning'
}
async function confirmDialog(opts: ConfirmOpts): Promise<boolean> {
  const modal = overlay.create(
    defineAsyncComponent(() => import('~/components/ConfirmModal.vue')),
    { props: opts },
  )
  const result = await modal.open()
  return Boolean(result)
}

const activeMoment = computed(
  () =>
    sortedMoments.value.find(
      (m) => currentTime.value >= m.start_s && currentTime.value <= m.end_s,
    ) ?? null,
)

watch(activeMoment, (m) => {
  if (!m) return
  selectedMomentId.value = m.id
  nextTick(() => {
    const el = momentListRef.value?.querySelector(`[data-moment-id="${m.id}"]`)
    el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  })
})

function timelinePercent(time: number): number {
  const dur = video.value?.duration_s || 1
  return Math.min(100, Math.max(0, (time / dur) * 100))
}

function parseTags(m: Moment): string[] {
  if (!m.llm_tags) return []
  try {
    const parsed = JSON.parse(m.llm_tags)
    if (Array.isArray(parsed)) return parsed.map(String)
  } catch {
    return m.llm_tags.split(',').map((t) => t.trim()).filter(Boolean)
  }
  return []
}

function prettyFeatures(m: Moment): string {
  if (!m.features) return ''
  try {
    return JSON.stringify(JSON.parse(m.features), null, 2)
  } catch {
    return m.features
  }
}
</script>

<template>
  <UDashboardPanel id="video-editor">
    <template #header>
      <UDashboardNavbar :title="video?.name || '…'">
        <template #leading>
          <UDashboardSidebarCollapse />
          <UButton
            variant="ghost"
            color="neutral"
            size="sm"
            icon="i-lucide-arrow-left"
            square
            aria-label="Back to assets"
            @click="router.push('/')"
          />
        </template>

        <template #trailing>
          <UBadge
            v-if="video?.game"
            :label="video.game"
            color="neutral"
            variant="soft"
            size="sm"
          />
        </template>

        <template #right>
          <UButton
            variant="outline"
            color="neutral"
            size="sm"
            icon="i-lucide-refresh-cw"
            label="Reprocess"
            @click="onReprocess"
          />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="error"
        color="error"
        variant="soft"
        icon="i-lucide-triangle-alert"
        :title="`Error: ${error}`"
      />

      <div v-else-if="loading" class="flex items-center justify-center gap-2 py-20">
        <UIcon name="i-lucide-refresh-cw" class="animate-spin" />
        <span>Loading…</span>
      </div>

      <div v-else class="flex flex-1 min-h-0 gap-4">
        <!-- Player + timeline + manual creation -->
        <div class="flex-1 flex flex-col gap-4 min-h-0">
          <video
            ref="playerRef"
            class="w-full min-h-0 max-h-full bg-black"
            controls
            playsinline
            :src="`/stream/video/${videoId}.mp4`"
            @timeupdate="onPlayerTimeUpdate"
          />

          <!-- Timeline strip — custom interactive bar -->
          <UCard
            v-if="video?.duration_s"
            :ui="{ body: 'p-3 sm:p-3' }"
          >
            <div class="relative h-6 bg-elevated overflow-hidden">
              <div
                v-for="m in sortedMoments"
                :key="m.id"
                class="absolute inset-y-0 cursor-pointer"
                :class="[
                  m.source === 'manual'
                    ? 'bg-blue-500/40 hover:bg-blue-500/60'
                    : 'bg-primary/40 hover:bg-primary/60',
                  selectedMomentId === m.id ? 'ring-1 ring-inset ring-primary' : '',
                ]"
                :style="{
                  left: `${timelinePercent(m.start_s)}%`,
                  width: `${timelinePercent(m.end_s) - timelinePercent(m.start_s)}%`,
                }"
                :title="m.title || 'untitled'"
                @click="selectMoment(m.id); onSeek(m.start_s)"
              />
            <div
              class="absolute inset-y-0 w-px bg-error pointer-events-none"
              :style="{ left: `${timelinePercent(currentTime)}%` }"
            />
          </div>
        </UCard>

        <!-- Manual moment creation controls -->
        <div
          v-if="video?.duration_s"
          class="flex items-center gap-3 flex-wrap"
        >
          <UButton
            size="sm"
            color="primary"
            variant="outline"
            icon="i-lucide-flag"
            label="Mark In"
            @click="markIn"
          />
          <UButton
            size="sm"
            color="primary"
            variant="outline"
            icon="i-lucide-flag-off"
            label="Mark Out"
            @click="markOut"
          />
          <div class="flex items-center gap-1 text-sm text-muted">
            <span v-if="manualStart != null">In {{ fmtTime(manualStart) }}</span>
            <span v-if="manualEnd != null">— Out {{ fmtTime(manualEnd) }}</span>
            <span v-else-if="manualStart == null && manualEnd == null" class="italic">No marks set</span>
          </div>
          <div class="flex-1" />
          <UInput
            v-model="manualTitle"
            size="sm"
            placeholder="Manual moment title"
            class="w-48"
          />
          <UButton
            size="sm"
            color="primary"
            icon="i-lucide-plus"
            label="Create"
            :disabled="manualStart == null || manualEnd == null"
            @click="onCreateManualMoment"
          />
          <UButton
            v-if="manualStart != null || manualEnd != null"
            size="sm"
            color="neutral"
            variant="ghost"
            icon="i-lucide-rotate-ccw"
            label="Clear"
            @click="manualStart = null; manualEnd = null; manualTitle = ''"
          />
        </div>
      </div>

      <!-- Moments sidebar -->
      <div class="w-96 flex flex-col gap-4 min-h-0 pl-2">
          <UDashboardToolbar>
            <template #left>
              <span class="text-sm font-medium">Moments ({{ sortedMoments.length }})</span>
              <USelect
                v-model="sortField"
                :items="SORT_OPTIONS"
                size="xs"
                class="w-32"
              />
              <UButton
                size="xs"
                variant="ghost"
                color="neutral"
                :icon="sortDirection === 'asc' ? 'i-lucide-arrow-up-narrow-wide' : 'i-lucide-arrow-down-wide-narrow'"
                @click="sortDirection = sortDirection === 'asc' ? 'desc' : 'asc'"
              />
            </template>
            <template #right>
              <UButton
                size="xs"
                variant="ghost"
                color="neutral"
                :icon="viewMode === 'small' ? 'i-lucide-panel-top-close' : 'i-lucide-panel-left-open'"
                :label="viewMode === 'small' ? 'Small' : 'Detailed'"
                @click="viewMode = viewMode === 'small' ? 'detailed' : 'small'"
              />
            </template>
          </UDashboardToolbar>

          <div
            ref="momentListRef"
            class="flex-1 min-h-0 overflow-y-auto flex flex-col gap-2"
          >
            <UCard
              v-for="m in sortedMoments"
              :key="m.id"
              :data-moment-id="m.id"
              :variant="selectedMomentId === m.id ? 'soft' : 'outline'"
              :ui="{ body: viewMode === 'small' ? 'p-2 sm:p-2' : 'p-3 sm:p-3' }"
              class="cursor-pointer"
              @click="selectMoment(m.id); onSeek(m.start_s)"
            >
              <!-- SMALL VIEW -->
              <template v-if="viewMode === 'small'">
                <div class="flex items-center gap-2 min-w-0">
                  <UButton
                    variant="link"
                    color="primary"
                    size="xs"
                    :padded="false"
                    :label="`${fmtTime(m.start_s)} – ${fmtTime(m.end_s)}`"
                    @click.stop="onSeek(m.start_s)"
                  />
                  <span class="truncate flex-1 min-w-0 text-sm">
                    {{ m.title || 'untitled' }}
                  </span>
                  <div class="flex items-center gap-1 shrink-0">
                    <UBadge
                      v-if="m.source === 'manual'"
                      label="manual"
                      class="bg-blue-500/15 text-blue-600 dark:bg-blue-400/15 dark:text-blue-300 border-blue-500/20"
                      size="xs"
                    />
                    <UBadge
                      v-if="m.clip_path && m.clip_stale"
                      label="stale"
                      color="warning"
                      variant="soft"
                      size="xs"
                    />
                    <UBadge
                      v-else-if="m.clip_path"
                      label="clipped"
                      color="success"
                      variant="soft"
                      size="xs"
                    />
                    <UBadge
                      :label="m.score.toFixed(2)"
                      color="neutral"
                      variant="subtle"
                      size="xs"
                    />
                  </div>
                </div>

                <!-- Editing controls for selected small card -->
                <div
                  v-if="selectedMomentId === m.id"
                  class="flex flex-col gap-2 mt-2"
                  @click.stop
                >
                  <USeparator />
                  <div
                    v-if="isEditingTitle"
                    class="flex items-center gap-2"
                  >
                    <UInput
                      v-model="titleDraft"
                      class="flex-1"
                      autofocus
                      size="sm"
                      @keyup.enter="saveTitle"
                      @keyup.esc="cancelEditTitle"
                    />
                    <UButton
                      size="sm"
                      color="primary"
                      icon="i-lucide-check"
                      square
                      aria-label="Save title"
                      @click.stop="saveTitle"
                    />
                    <UButton
                      size="sm"
                      color="neutral"
                      variant="ghost"
                      icon="i-lucide-x"
                      square
                      aria-label="Cancel"
                      @click.stop="cancelEditTitle"
                    />
                  </div>
                  <div
                    v-else
                    class="cursor-text text-sm"
                    @click.stop="beginEditTitle(m)"
                  >
                    <span class="text-muted">Click to edit title</span>
                  </div>

                  <UFormField label="Start" size="xs">
                    <UButtonGroup size="xs" class="flex">
                      <UButton variant="outline" color="neutral" label="-1s" @click="nudgeStart(-1)" />
                      <UButton variant="outline" color="neutral" label="-.1" @click="nudgeStart(-0.1)" />
                      <UButton variant="outline" color="primary" icon="i-lucide-move-horizontal" square @click="setStartToPlayhead()" />
                      <UButton variant="outline" color="neutral" label="+.1" @click="nudgeStart(0.1)" />
                      <UButton variant="outline" color="neutral" label="+1s" @click="nudgeStart(1)" />
                    </UButtonGroup>
                  </UFormField>

                  <UFormField label="End" size="xs">
                    <UButtonGroup size="xs" class="flex">
                      <UButton variant="outline" color="neutral" label="-1s" @click="nudgeEnd(-1)" />
                      <UButton variant="outline" color="neutral" label="-.1" @click="nudgeEnd(-0.1)" />
                      <UButton variant="outline" color="primary" icon="i-lucide-move-horizontal" square @click="setEndToPlayhead()" />
                      <UButton variant="outline" color="neutral" label="+.1" @click="nudgeEnd(0.1)" />
                      <UButton variant="outline" color="neutral" label="+1s" @click="nudgeEnd(1)" />
                    </UButtonGroup>
                  </UFormField>

                  <div class="flex items-center gap-2 flex-wrap">
                    <UButton
                      v-if="!m.clip_path"
                      size="xs"
                      color="primary"
                      icon="i-lucide-scissors"
                      label="Clip"
                      :loading="clippingIds.has(m.id)"
                      @click="onCreateClip(m.id)"
                    />
                    <UButton
                      v-else-if="m.clip_stale"
                      size="xs"
                      color="warning"
                      icon="i-lucide-refresh-cw"
                      label="Update"
                      :loading="clippingIds.has(m.id)"
                      @click="onCreateClip(m.id)"
                    />
                    <UButton
                      v-if="!m.clip_stale"
                      size="xs"
                      color="neutral"
                      variant="outline"
                      icon="i-lucide-download"
                      label="Download"
                      :disabled="!m.clip_path"
                      :to="m.clip_path ? `/clip/${m.id}.mp4?download=1` : undefined"
                      :external="!!m.clip_path"
                      :download="!!m.clip_path"
                    />
                    <UButton
                      size="xs"
                      color="neutral"
                      variant="outline"
                      icon="i-lucide-share-2"
                      label="Share"
                      :disabled="!m.clip_path || m.clip_stale"
                      @click="onShareClip(m.id)"
                    />
                    <UButton
                      size="xs"
                      color="error"
                      variant="soft"
                      icon="i-lucide-trash-2"
                      label="Delete"
                      @click="onDeleteMoment(m.id, m.title)"
                    />
                  </div>
                </div>
              </template>

              <!-- DETAILED VIEW -->
              <template v-else>
                <div class="flex flex-col gap-2">
                  <div class="flex items-center justify-between gap-2">
                    <UButton
                      variant="link"
                      color="primary"
                      size="sm"
                      :padded="false"
                      :label="`${fmtTime(m.start_s)} – ${fmtTime(m.end_s)}`"
                      @click.stop="onSeek(m.start_s)"
                    />
                    <div class="flex items-center gap-1 flex-wrap justify-end">
                      <UBadge
                        v-if="m.source === 'manual'"
                        label="manual"
                        class="bg-blue-500/15 text-blue-600 dark:bg-blue-400/15 dark:text-blue-300 border-blue-500/20"
                        size="sm"
                      />
                      <UBadge
                        v-if="m.clip_path && m.clip_stale"
                        label="stale"
                        color="warning"
                        variant="soft"
                        size="sm"
                      />
                      <UBadge
                        v-else-if="m.clip_path"
                        label="clipped"
                        color="success"
                        variant="soft"
                        size="sm"
                      />
                      <UBadge
                        v-if="m.user_rating != null"
                        :label="`${m.user_rating}★`"
                        color="warning"
                        variant="soft"
                        size="sm"
                      />
                      <UBadge
                        :label="`score ${m.score.toFixed(2)}`"
                        color="neutral"
                        variant="subtle"
                        size="sm"
                      />
                      <UBadge
                        v-if="m.llm_rank != null"
                        :label="`rank #${m.llm_rank}`"
                        color="primary"
                        variant="subtle"
                        size="sm"
                      />
                    </div>
                  </div>

                  <!-- Title editing -->
                  <div
                    v-if="selectedMomentId === m.id && isEditingTitle"
                    class="flex items-center gap-2"
                    @click.stop
                  >
                    <UInput
                      v-model="titleDraft"
                      class="flex-1"
                      autofocus
                      @keyup.enter="saveTitle"
                      @keyup.esc="cancelEditTitle"
                    />
                    <UButton
                      size="sm"
                      color="primary"
                      icon="i-lucide-check"
                      square
                      aria-label="Save title"
                      @click.stop="saveTitle"
                    />
                    <UButton
                      size="sm"
                      color="neutral"
                      variant="ghost"
                      icon="i-lucide-x"
                      square
                      aria-label="Cancel"
                      @click.stop="cancelEditTitle"
                    />
                  </div>
                  <div
                    v-else
                    class="cursor-text font-medium"
                    @click.stop="beginEditTitle(m)"
                  >
                    <span>{{ m.title || 'untitled' }}</span>
                  </div>

                  <!-- LLM metadata -->
                  <p v-if="m.llm_desc" class="text-sm text-muted">
                    {{ m.llm_desc }}
                  </p>

                  <p v-if="m.llm_contents" class="text-sm">
                    {{ m.llm_contents }}
                  </p>

                  <div v-if="parseTags(m).length" class="flex flex-wrap gap-1">
                    <UBadge
                      v-for="tag in parseTags(m)"
                      :key="tag"
                      :label="tag"
                      color="primary"
                      variant="soft"
                      size="xs"
                    />
                  </div>

                  <!-- Expandable details -->
                  <details v-if="m.transcript" class="text-sm">
                    <summary class="cursor-pointer text-muted hover:text-default select-none">
                      Transcript
                    </summary>
                    <p class="mt-1 text-muted whitespace-pre-wrap">{{ m.transcript }}</p>
                  </details>

                  <details v-if="m.features" class="text-sm">
                    <summary class="cursor-pointer text-muted hover:text-default select-none">
                      Score details
                    </summary>
                    <pre class="mt-1 text-xs text-muted bg-elevated p-2 rounded overflow-x-auto">{{ prettyFeatures(m) }}</pre>
                  </details>

                  <!-- Editing controls -->
                  <div
                    v-if="selectedMomentId === m.id"
                    class="flex flex-col gap-2"
                    @click.stop
                  >
                    <USeparator />

                    <UFormField label="Start">
                      <UButtonGroup size="sm" class="flex">
                        <UButton variant="outline" color="neutral" label="-1s" @click="nudgeStart(-1)" />
                        <UButton variant="outline" color="neutral" label="-.1" @click="nudgeStart(-0.1)" />
                        <UButton variant="outline" color="primary" icon="i-lucide-move-horizontal" square @click="setStartToPlayhead()" />
                        <UButton variant="outline" color="neutral" label="+.1" @click="nudgeStart(0.1)" />
                        <UButton variant="outline" color="neutral" label="+1s" @click="nudgeStart(1)" />
                      </UButtonGroup>
                    </UFormField>

                    <UFormField label="End">
                      <UButtonGroup size="sm" class="flex">
                        <UButton variant="outline" color="neutral" label="-1s" @click="nudgeEnd(-1)" />
                        <UButton variant="outline" color="neutral" label="-.1" @click="nudgeEnd(-0.1)" />
                        <UButton variant="outline" color="primary" icon="i-lucide-move-horizontal" square @click="setEndToPlayhead()" />
                        <UButton variant="outline" color="neutral" label="+.1" @click="nudgeEnd(0.1)" />
                        <UButton variant="outline" color="neutral" label="+1s" @click="nudgeEnd(1)" />
                      </UButtonGroup>
                    </UFormField>

                    <div class="flex items-center gap-2 flex-wrap">
                      <UButton
                        v-if="!m.clip_path"
                        size="sm"
                        color="primary"
                        icon="i-lucide-scissors"
                        label="Clip"
                        :loading="clippingIds.has(m.id)"
                        @click="onCreateClip(m.id)"
                      />
                      <UButton
                        v-else-if="m.clip_stale"
                        size="sm"
                        color="warning"
                        icon="i-lucide-refresh-cw"
                        label="Update"
                        :loading="clippingIds.has(m.id)"
                        @click="onCreateClip(m.id)"
                      />
                      <UButton
                        v-if="!m.clip_stale"
                        size="sm"
                        color="neutral"
                        variant="outline"
                        icon="i-lucide-download"
                        label="Download"
                        :disabled="!m.clip_path"
                        :to="m.clip_path ? `/clip/${m.id}.mp4?download=1` : undefined"
                        :external="!!m.clip_path"
                        :download="!!m.clip_path"
                      />
                      <UButton
                        size="sm"
                        color="neutral"
                        variant="outline"
                        icon="i-lucide-share-2"
                        label="Share"
                        :disabled="!m.clip_path || m.clip_stale"
                        @click="onShareClip(m.id)"
                      />
                      <UButton
                        size="sm"
                        color="error"
                        variant="soft"
                        icon="i-lucide-trash-2"
                        label="Delete"
                        @click="onDeleteMoment(m.id, m.title)"
                      />
                    </div>
                  </div>
                </div>
              </template>
            </UCard>
          </div>
        </div>
      </div>
    </template>
  </UDashboardPanel>
</template>
