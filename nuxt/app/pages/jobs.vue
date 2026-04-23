<script setup lang="ts">
import { h, resolveComponent } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import type { JobItem } from '~/composables/useDashboard'

const UBadge = resolveComponent('UBadge')

const { jobs, loading, reload } = useDashboard()

const activeJobs = computed(() =>
  jobs.value.filter((j) => j.status === 'running' || j.status === 'pending'),
)
const doneJobs = computed(() => jobs.value.filter((j) => j.status === 'done'))
const failedJobs = computed(() => jobs.value.filter((j) => j.status === 'failed'))

function jobBadgeColor(status: string): 'success' | 'error' | 'primary' | 'neutral' {
  if (status === 'done') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'running') return 'primary'
  return 'neutral'
}

function fmtDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const columns: TableColumn<JobItem>[] = [
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) =>
      h(UBadge, {
        label: row.original.status,
        color: jobBadgeColor(row.original.status),
        variant: 'soft',
        size: 'sm',
      }),
  },
  {
    accessorKey: 'type',
    header: 'Stage',
  },
  {
    accessorKey: 'video_name',
    header: 'Video',
    cell: ({ row }) =>
      row.original.video_name || `video #${row.original.video_id ?? '?'}`,
  },
  {
    accessorKey: 'game',
    header: 'Game',
    cell: ({ row }) => row.original.game ?? '—',
  },
  {
    accessorKey: 'error_message',
    header: 'Error',
    cell: ({ row }) =>
      row.original.error_message
        ? h(UBadge, {
          label: row.original.error_message,
          color: 'error',
          variant: 'soft',
          size: 'sm',
        })
        : '',
  },
  {
    accessorKey: 'created_at',
    header: 'Created',
    cell: ({ row }) => fmtDate(row.original.created_at),
  },
]

const stats = computed(() => [
  {
    title: String(activeJobs.value.length),
    description: 'Active',
    icon: 'i-lucide-circle-play',
    variant: 'soft' as const,
    highlightColor: 'primary' as const,
  },
  {
    title: String(doneJobs.value.length),
    description: 'Done',
    icon: 'i-lucide-circle-check',
    variant: 'soft' as const,
    highlightColor: 'success' as const,
  },
  {
    title: String(failedJobs.value.length),
    description: 'Failed',
    icon: 'i-lucide-circle-x',
    variant: 'soft' as const,
    highlightColor: 'error' as const,
  },
])
</script>

<template>
  <UDashboardPanel id="jobs">
    <template #header>
      <UDashboardNavbar title="Jobs" icon="i-lucide-list-checks">
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
      <UPageGrid>
        <UPageCard
          v-for="s in stats"
          :key="s.description"
          :title="s.title"
          :description="s.description"
          :icon="s.icon"
          :variant="s.variant"
          :highlight-color="s.highlightColor"
          highlight
        />
      </UPageGrid>

      <UAlert
        v-if="!loading && jobs.length === 0"
        color="neutral"
        variant="soft"
        icon="i-lucide-list-checks"
        title="No jobs yet"
        description="Run a pipeline stage to see activity here."
      />

      <UCard v-else>
        <UTable :columns="columns" :data="jobs" :loading="loading" sticky />
      </UCard>
    </template>
  </UDashboardPanel>
</template>
