import { ref, computed } from 'vue'
import { PANEL_WIDTH_KEY } from '../constants'

export const MIN_PANEL_WIDTH = 180
export const MAX_PANEL_WIDTH = 480
const DEFAULT_PANEL_WIDTH = 260

function clamp(v: number): number {
  return Math.max(MIN_PANEL_WIDTH, Math.min(MAX_PANEL_WIDTH, v))
}

function loadWidth(): number {
  try {
    const raw = localStorage.getItem(PANEL_WIDTH_KEY)
    if (raw !== null) {
      const n = Number(raw)
      if (Number.isFinite(n)) return clamp(n)
    }
  } catch {
    // localStorage unavailable
  }
  return DEFAULT_PANEL_WIDTH
}

const panelWidth = ref(loadWidth())

const gridCols = computed(() => `64px ${panelWidth.value}px minmax(0, 1fr)`)

function setPanelWidth(w: number): void {
  panelWidth.value = clamp(w)
  try {
    localStorage.setItem(PANEL_WIDTH_KEY, String(panelWidth.value))
  } catch {
    // localStorage unavailable
  }
}

export function usePanelWidth() {
  return { panelWidth, gridCols, setPanelWidth }
}
