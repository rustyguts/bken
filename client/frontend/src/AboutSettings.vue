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

    <div class="card bg-base-200/40 border border-base-content/10">
      <div class="card-body p-4">
        <h3 class="card-title text-sm">Build Information</h3>
        <table class="table table-xs mt-2">
          <tbody>
            <tr>
              <td class="opacity-60">Git Commit</td>
              <td class="font-mono text-right">{{ shortCommit(buildInfo.commit) }}<span v-if="buildInfo.dirty"> (modified)</span></td>
            </tr>
            <tr>
              <td class="opacity-60">Build Time</td>
              <td class="font-mono text-right">{{ buildInfo.build_time || 'unknown' }}</td>
            </tr>
            <tr>
              <td class="opacity-60">Go Version</td>
              <td class="font-mono text-right">{{ buildInfo.go_version || 'unknown' }}</td>
            </tr>
            <tr>
              <td class="opacity-60">Target</td>
              <td class="font-mono text-right">{{ buildInfo.goos || 'unknown' }}/{{ buildInfo.goarch || 'unknown' }}</td>
            </tr>
            <tr>
              <td class="opacity-60">Runtime</td>
              <td class="font-mono text-right">{{ envInfo.platform || 'unknown' }}/{{ envInfo.arch || 'unknown' }} ({{ envInfo.buildType || 'unknown' }})</td>
            </tr>
            <tr>
              <td class="opacity-60">Browser Engine</td>
              <td class="font-mono text-right">{{ browserInfo }}</td>
            </tr>
          </tbody>
        </table>

        <details class="collapse collapse-arrow bg-base-100 mt-3">
          <summary class="collapse-title text-xs py-2 min-h-0">User Agent</summary>
          <div class="collapse-content">
            <p class="break-all font-mono text-xs opacity-60">{{ userAgent }}</p>
          </div>
        </details>
      </div>
    </div>
  </section>
</template>
