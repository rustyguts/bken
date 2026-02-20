import { ref } from 'vue'
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

const currentTheme = ref('dark')

/** Restore theme from config. Call once during settings mount. */
async function restoreFromConfig(): Promise<void> {
  const cfg = await GetConfig()
  const validTheme = THEMES.some(t => t.name === cfg.theme)
  if (validTheme) {
    currentTheme.value = cfg.theme
    document.documentElement.setAttribute('data-theme', cfg.theme)
    localStorage.setItem('bken-theme', cfg.theme)
  }
}

/** Apply a theme: update DOM, localStorage, and persist to config. */
async function applyTheme(theme: string): Promise<void> {
  currentTheme.value = theme
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem('bken-theme', theme)
  const cfg = await GetConfig()
  await SaveConfig({ ...cfg, theme })
}

export function useTheme() {
  return { THEMES, currentTheme, applyTheme, restoreFromConfig }
}
