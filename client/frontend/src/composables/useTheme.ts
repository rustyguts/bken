import { ref, onBeforeUnmount } from 'vue'
import { GetConfig, SaveConfig } from '../config'

const THEMES = [
  // Light
  { name: 'light', label: 'Light' },
  { name: 'cupcake', label: 'Cupcake' },
  { name: 'bumblebee', label: 'Bumblebee' },
  { name: 'emerald', label: 'Emerald' },
  { name: 'corporate', label: 'Corporate' },
  { name: 'retro', label: 'Retro' },
  { name: 'cyberpunk', label: 'Cyberpunk' },
  { name: 'valentine', label: 'Valentine' },
  { name: 'garden', label: 'Garden' },
  { name: 'lofi', label: 'Lo-Fi' },
  { name: 'pastel', label: 'Pastel' },
  { name: 'fantasy', label: 'Fantasy' },
  { name: 'wireframe', label: 'Wireframe' },
  { name: 'cmyk', label: 'CMYK' },
  { name: 'autumn', label: 'Autumn' },
  { name: 'acid', label: 'Acid' },
  { name: 'lemonade', label: 'Lemonade' },
  { name: 'winter', label: 'Winter' },
  { name: 'nord', label: 'Nord' },
  { name: 'caramellatte', label: 'Caramel Latte' },
  { name: 'silk', label: 'Silk' },
  // Dark
  { name: 'dark', label: 'Dark' },
  { name: 'synthwave', label: 'Synthwave' },
  { name: 'halloween', label: 'Halloween' },
  { name: 'forest', label: 'Forest' },
  { name: 'aqua', label: 'Aqua' },
  { name: 'black', label: 'Black' },
  { name: 'luxury', label: 'Luxury' },
  { name: 'dracula', label: 'Dracula' },
  { name: 'business', label: 'Business' },
  { name: 'night', label: 'Night' },
  { name: 'coffee', label: 'Coffee' },
  { name: 'dim', label: 'Dim' },
  { name: 'sunset', label: 'Sunset' },
  { name: 'abyss', label: 'Abyss' },
] as const

/** Theme mode: a specific theme name, or 'system' to follow OS preference. */
type ThemeMode = 'system' | string

const currentTheme = ref('dark')
const themeMode = ref<ThemeMode>('dark')

/** Detect the system preferred theme (light or dark). */
function systemTheme(): string {
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) {
    return 'light'
  }
  return 'dark'
}

/** Resolve which DaisyUI theme to apply given the current mode. */
function resolveTheme(mode: ThemeMode): string {
  return mode === 'system' ? systemTheme() : mode
}

let mediaQuery: MediaQueryList | null = null
let mediaHandler: ((e: MediaQueryListEvent) => void) | null = null

function applyToDOM(theme: string): void {
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem('bken-theme', theme)
}

/** Listen for OS-level theme changes when in system mode. */
function startSystemListener(): void {
  stopSystemListener()
  if (typeof window === 'undefined') return
  mediaQuery = window.matchMedia('(prefers-color-scheme: light)')
  mediaHandler = () => {
    if (themeMode.value === 'system') {
      const resolved = systemTheme()
      currentTheme.value = resolved
      applyToDOM(resolved)
    }
  }
  mediaQuery.addEventListener('change', mediaHandler)
}

function stopSystemListener(): void {
  if (mediaQuery && mediaHandler) {
    mediaQuery.removeEventListener('change', mediaHandler)
  }
  mediaQuery = null
  mediaHandler = null
}

/** Restore theme from config. Call once during settings mount. */
async function restoreFromConfig(): Promise<void> {
  const cfg = await GetConfig()
  const savedMode = (cfg as unknown as Record<string, unknown>).theme_mode as string | undefined
  const validTheme = THEMES.some(t => t.name === cfg.theme)

  if (savedMode === 'system') {
    themeMode.value = 'system'
    const resolved = systemTheme()
    currentTheme.value = resolved
    applyToDOM(resolved)
    startSystemListener()
  } else if (validTheme) {
    themeMode.value = cfg.theme
    currentTheme.value = cfg.theme
    applyToDOM(cfg.theme)
  }
}

/** Apply a specific theme: update DOM, localStorage, and persist to config. */
async function applyTheme(theme: string): Promise<void> {
  stopSystemListener()
  themeMode.value = theme
  currentTheme.value = theme
  applyToDOM(theme)
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, theme })
}

/** Set theme mode to 'system' to follow OS preference. */
async function setSystemMode(): Promise<void> {
  themeMode.value = 'system'
  const resolved = systemTheme()
  currentTheme.value = resolved
  applyToDOM(resolved)
  startSystemListener()
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, theme: resolved })
}

export function useTheme() {
  return { THEMES, currentTheme, themeMode, applyTheme, setSystemMode, restoreFromConfig, stopSystemListener }
}
