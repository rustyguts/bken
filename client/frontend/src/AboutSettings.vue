<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Environment } from '../wailsjs/runtime/runtime'
import { GetBuildInfo } from './config'
import { Info } from 'lucide-vue-next'

type BuildInfo = {
  commit: string
  build_time: string
  go_version: string
  goos: string
  goarch: string
  dirty: boolean
}

type RuntimeEnv = {
  buildType?: string
  platform?: string
  arch?: string
}

const buildInfo = ref<BuildInfo>({
  commit: 'unknown',
  build_time: '',
  go_version: '',
  goos: '',
  goarch: '',
  dirty: false,
})

const envInfo = ref<RuntimeEnv>({})
const browserInfo = ref('Unknown')
const userAgent = ref('')

function parseBrowserVersion(ua: string): string {
  const webkit = ua.match(/AppleWebKit\/([\d.]+)/i)
  const chrome = ua.match(/Chrome\/([\d.]+)/i)
  const safari = ua.match(/Version\/([\d.]+).*Safari/i)

  if (chrome) return `Chromium ${chrome[1]}`
  if (safari) return `Safari ${safari[1]}`
  if (webkit) return `WebKit ${webkit[1]}`
  return 'Unknown'
}

function shortCommit(commit: string): string {
  if (!commit) return 'unknown'
  return commit.length > 12 ? commit.slice(0, 12) : commit
}

onMounted(async () => {
  const [build, env] = await Promise.all([
    GetBuildInfo(),
    Environment(),
  ])

  buildInfo.value = build
  envInfo.value = env ?? {}
  userAgent.value = navigator.userAgent
  browserInfo.value = parseBrowserVersion(userAgent.value)
})
</script>

<template>
  <section>
    <div class="flex items-center gap-2 mb-3">
      <Info class="w-4 h-4 text-primary shrink-0" aria-hidden="true" />
      <span class="text-xs font-semibold uppercase tracking-wider opacity-60">About</span>
    </div>

    <div class="card bg-base-200/40 border border-base-content/10 p-4">
      <h3 class="text-sm font-semibold">Build Information</h3>
      <div class="mt-3 grid gap-2 text-xs">
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Git Commit</span>
          <span class="font-mono">{{ shortCommit(buildInfo.commit) }}<span v-if="buildInfo.dirty"> (modified)</span></span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Build Time</span>
          <span class="font-mono">{{ buildInfo.build_time || 'unknown' }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Go Version</span>
          <span class="font-mono">{{ buildInfo.go_version || 'unknown' }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Target</span>
          <span class="font-mono">{{ buildInfo.goos || 'unknown' }}/{{ buildInfo.goarch || 'unknown' }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Runtime</span>
          <span class="font-mono">{{ envInfo.platform || 'unknown' }}/{{ envInfo.arch || 'unknown' }} ({{ envInfo.buildType || 'unknown' }})</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="opacity-60">Browser Engine</span>
          <span class="font-mono">{{ browserInfo }}</span>
        </div>
      </div>

      <details class="mt-3 text-xs">
        <summary class="cursor-pointer opacity-70">User Agent</summary>
        <p class="mt-2 break-all font-mono opacity-60">{{ userAgent }}</p>
      </details>
    </div>
  </section>
</template>
