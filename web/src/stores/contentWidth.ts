import { create } from 'zustand'

export const CONTENT_WIDTH_KEY = 'nv_main_content_width'
export const MIN_CONTENT_WIDTH = 960
export const MAX_CONTENT_WIDTH = 1920
export const STEP_CONTENT_WIDTH = 20
export const DEFAULT_CONTENT_WIDTH = 1440
export const MOBILE_BREAKPOINT = 768

function clampWidth(width: number) {
  return Math.min(MAX_CONTENT_WIDTH, Math.max(MIN_CONTENT_WIDTH, width))
}

function readPersistedWidth(): number {
  if (typeof window === 'undefined') return DEFAULT_CONTENT_WIDTH
  try {
    const raw = window.localStorage.getItem(CONTENT_WIDTH_KEY)
    if (raw === null || raw === '') return DEFAULT_CONTENT_WIDTH
    const parsed = parseInt(raw, 10)
    if (!Number.isFinite(parsed)) return DEFAULT_CONTENT_WIDTH
    return clampWidth(parsed)
  } catch {
    return DEFAULT_CONTENT_WIDTH
  }
}

interface ContentWidthStore {
  width: number
  setWidth: (width: number) => void
  resetWidth: () => void
}

/**
 * Desktop content max-width for #main-scroll-container. Persisted under
 * nv_main_content_width; mobile (<=768px) intentionally suppresses the inline
 * max-width so the native layout is always used.
 */
export const useContentWidthStore = create<ContentWidthStore>((set) => ({
  width: readPersistedWidth(),
  setWidth: (width) => {
    const clamped = clampWidth(width)
    set({ width: clamped })
    try {
      window.localStorage.setItem(CONTENT_WIDTH_KEY, String(clamped))
    } catch {
      // Storage may be unavailable in privacy-restricted contexts.
    }
  },
  resetWidth: () => {
    set({ width: DEFAULT_CONTENT_WIDTH })
    try {
      window.localStorage.removeItem(CONTENT_WIDTH_KEY)
    } catch {
      // Storage may be unavailable in privacy-restricted contexts.
    }
  },
}))
