<script setup lang="ts">
import type { NavigationMenuItem } from '@nuxt/ui'

const links = computed<NavigationMenuItem[][]>(() => [
  [
    { label: 'Assets', icon: 'i-lucide-library', to: '/' },
    { label: 'Libraries', icon: 'i-lucide-folder-open', to: '/libraries' },
    { label: 'Jobs', icon: 'i-lucide-list-checks', to: '/jobs' },
  ],
])
</script>

<template>
  <UDashboardGroup>
    <UDashboardSidebar id="vidpipe-sidebar" collapsible resizable>
      <template #header="{ collapsed }">
        <UIcon name="i-lucide-zap" class="size-5 text-primary shrink-0" />
        <span v-if="!collapsed" class="font-semibold truncate">vidpipe</span>
      </template>

      <template #default="{ collapsed }">
        <UNavigationMenu
          :items="links"
          orientation="vertical"
          :collapsed="collapsed"
        />
      </template>

      <template #footer>
        <UColorModeButton />
      </template>
    </UDashboardSidebar>

    <slot />
  </UDashboardGroup>
</template>
