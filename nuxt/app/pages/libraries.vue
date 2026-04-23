<script setup lang="ts">
import type { LibraryItem } from '~/composables/useDashboard'

const toast = useToast()
const overlay = useOverlay()

const libraries = ref<LibraryItem[]>([])
const loading = ref(false)
const error = ref('')

const showAddForm = ref(false)
const newPath = ref('')
const newName = ref('')
const newInterval = ref(60)

async function fetchLibraries() {
  loading.value = true
  error.value = ''
  try {
    const data = await $fetch<{ libraries: LibraryItem[] }>('/api/libraries')
    libraries.value = data.libraries
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function addLibrary() {
  error.value = ''
  if (!newPath.value.trim()) {
    toast.add({
      title: 'Path required',
      color: 'error',
      icon: 'i-lucide-triangle-alert',
    })
    return
  }
  try {
    await $fetch('/api/libraries', {
      method: 'POST',
      body: {
        path: newPath.value.trim(),
        name: newName.value.trim() || undefined,
        scan_interval_minutes: newInterval.value,
      },
    })
    showAddForm.value = false
    newPath.value = ''
    newName.value = ''
    newInterval.value = 60
    toast.add({ title: 'Library created', color: 'success', icon: 'i-lucide-check' })
    await fetchLibraries()
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    toast.add({ title: 'Failed to add library', description: msg, color: 'error' })
  }
}

async function removeLibrary(lib: LibraryItem) {
  const modal = overlay.create(
    defineAsyncComponent(() => import('~/components/ConfirmModal.vue')),
    {
      props: {
        title: 'Deactivate library?',
        description: `"${lib.name}" will be deactivated. Its videos will be marked as missing.`,
        confirmLabel: 'Deactivate',
        color: 'error',
      },
    },
  )
  const ok = await modal.open()
  if (!ok) return
  await $fetch(`/api/libraries/${lib.id}`, { method: 'DELETE' })
  toast.add({ title: 'Library deactivated', color: 'neutral', icon: 'i-lucide-trash-2' })
  await fetchLibraries()
}

async function syncLibrary(id: number) {
  await $fetch(`/api/libraries/${id}/sync`, { method: 'POST' })
  toast.add({ title: 'Sync queued', color: 'primary', icon: 'i-lucide-refresh-cw' })
}

onMounted(fetchLibraries)
</script>

<template>
  <UDashboardPanel id="libraries">
    <template #header>
      <UDashboardNavbar title="Libraries" icon="i-lucide-folder-open">
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
            @click="fetchLibraries"
          />
          <UButton
            color="primary"
            size="sm"
            :icon="showAddForm ? 'i-lucide-x' : 'i-lucide-plus'"
            :label="showAddForm ? 'Cancel' : 'Add library'"
            @click="showAddForm = !showAddForm"
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
        :title="error"
      />

      <UCard v-if="showAddForm">
        <UForm :state="{ newPath, newName, newInterval }" class="flex flex-col gap-3" @submit="addLibrary">
          <UFormField label="Path" required>
            <UInput v-model="newPath" placeholder="/path/to/library" class="w-full" />
          </UFormField>
          <UFormField label="Name" help="Optional display name (defaults to the directory name).">
            <UInput v-model="newName" placeholder="Gaming archive" class="w-full" />
          </UFormField>
          <UFormField label="Scan interval (minutes)">
            <UInput v-model.number="newInterval" type="number" min="1" class="w-full" />
          </UFormField>
          <div class="flex justify-end">
            <UButton color="primary" icon="i-lucide-plus" label="Create library" type="submit" />
          </div>
        </UForm>
      </UCard>

      <UAlert
        v-if="!loading && libraries.length === 0"
        color="neutral"
        variant="soft"
        icon="i-lucide-folder-open"
        title="No libraries configured yet"
        description="Add one to start watching a directory."
      />

      <UPageList v-else>
        <UPageCard
          v-for="lib in libraries"
          :key="lib.id"
          variant="subtle"
          :title="lib.name"
          :description="lib.path"
        >
          <template #footer>
            <div class="flex items-center justify-between gap-3 flex-wrap">
              <div class="flex items-center gap-2 flex-wrap">
                <UBadge
                  :label="lib.active ? 'active' : 'inactive'"
                  :color="lib.active ? 'success' : 'neutral'"
                  variant="soft"
                  size="sm"
                />
                <UBadge
                  :label="`${lib.video_count} videos`"
                  color="neutral"
                  variant="soft"
                  size="sm"
                />
                <UBadge
                  v-if="lib.missing_count > 0"
                  :label="`${lib.missing_count} missing`"
                  color="error"
                  variant="soft"
                  size="sm"
                />
                <UBadge
                  :label="lib.last_scan_at ? `last scan ${lib.last_scan_at.slice(0, 19).replace('T', ' ')}` : 'never scanned'"
                  color="neutral"
                  variant="outline"
                  size="sm"
                />
              </div>

              <div class="flex items-center gap-2">
                <UButton
                  v-if="lib.active"
                  size="sm"
                  variant="outline"
                  color="neutral"
                  icon="i-lucide-refresh-cw"
                  label="Sync now"
                  @click="syncLibrary(lib.id)"
                />
                <UButton
                  v-if="lib.active"
                  size="sm"
                  variant="soft"
                  color="error"
                  icon="i-lucide-trash-2"
                  label="Remove"
                  @click="removeLibrary(lib)"
                />
              </div>
            </div>
          </template>
        </UPageCard>
      </UPageList>
    </template>
  </UDashboardPanel>
</template>
