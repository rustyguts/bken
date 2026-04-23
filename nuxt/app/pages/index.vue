<script setup lang="ts">
const { videos, loading, error, reload, filters, fetchLibraries } = useDashboard()

const stages = ['audio', 'transcribe', 'events', 'score'] as const
const libraries = ref<{ id: number; name: string }[]>([])

onMounted(async () => {
  const all = await fetchLibraries()
  libraries.value = all.map((l) => ({ id: l.id, name: l.name }))
})

// Reka's Select primitive forbids empty-string values. Bridge the
// filter's string model (where `''` means "all") to `undefined`, which
// shows the placeholder instead.
const librarySelection = computed({
  get: () => filters.value.library_id || undefined,
  set: (v: string | undefined) => {
    filters.value.library_id = v ?? ''
  },
})

const libraryItems = computed(() =>
  libraries.value.map((l) => ({ label: l.name, value: String(l.id) })),
)

const showMissing = computed({
  get: () => filters.value.missing === '1',
  set: (v: boolean) => {
    filters.value.missing = v ? '1' : '0'
  },
})

function fmtDur(sec: number | null): string {
  if (!sec) return '–'
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
  return `${m}:${String(ss).padStart(2, '0')}`
}
</script>

<template>
  <UDashboardPanel id="assets">
    <template #header>
      <UDashboardNavbar title="Assets" icon="i-lucide-library">
        <template #leading>
          <UDashboardSidebarCollapse />
        </template>

        <template #right>
          <UButton
            color="primary"
            variant="outline"
            size="sm"
            icon="i-lucide-refresh-cw"
            :loading="loading"
            label="Refresh"
            @click="reload"
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
        :title="`API error: ${error}`"
      />

      <template v-if="!error">
        <!-- Filters -->
        <div class="flex flex-wrap items-center gap-3 mb-4">
          <USelect
            v-model="librarySelection"
            :items="libraryItems"
            placeholder="All libraries"
            class="w-48"
            size="sm"
          />
          <UButton
            v-if="librarySelection"
            size="sm"
            variant="ghost"
            color="neutral"
            icon="i-lucide-x"
            label="Clear"
            @click="librarySelection = undefined"
          />
          <UCheckbox v-model="showMissing" label="Show missing" />
        </div>

        <UPageColumns v-if="!loading && videos.length === 0">
          <UCard>
            <div class="flex flex-col items-center gap-2">
              <UIcon name="i-lucide-library" class="size-8" />
              <p>No videos ingested yet.</p>
            </div>
          </UCard>
        </UPageColumns>

        <UPageGrid v-else>
        <UPageCard
          v-for="v in videos"
          :key="v.id"
          :to="`/video/${v.id}`"
          :title="v.name"
          variant="subtle"
          spotlight
        >
          <template #body>
            <div class="aspect-video overflow-hidden">
              <img
                v-if="v.thumb_url"
                :src="v.thumb_url"
                class="size-full object-cover"
                loading="lazy"
                alt="Thumbnail"
              >
            </div>
          </template>

          <template #footer>
            <div class="flex flex-col gap-2">
              <div class="flex items-center gap-2">
                <UBadge
                  v-if="v.missing"
                  label="missing"
                  color="error"
                  variant="soft"
                  size="sm"
                />
                <UBadge
                  :label="v.game || 'Unknown'"
                  color="neutral"
                  variant="soft"
                  size="sm"
                />
                <UBadge
                  :label="fmtDur(v.duration_s)"
                  color="neutral"
                  variant="outline"
                  size="sm"
                />
                <UBadge
                  :label="`${v.moment_count} moments`"
                  color="primary"
                  variant="soft"
                  size="sm"
                />
              </div>

              <div class="flex flex-wrap gap-1">
                <UBadge
                  v-for="stage in stages"
                  :key="stage"
                  :label="stage"
                  :color="v.status[stage] ? 'success' : 'neutral'"
                  variant="soft"
                  size="sm"
                />
              </div>
            </div>
          </template>
        </UPageCard>
      </UPageGrid>
      </template>
    </template>
  </UDashboardPanel>
</template>
