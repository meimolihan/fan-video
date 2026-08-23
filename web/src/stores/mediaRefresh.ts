import { create } from 'zustand'

/**
 * 全局海报/封面"版本戳"。
 *
 * 用途：当刮削完成（WS: scrape_completed）或用户手动替换元数据时，
 * 调用 bumpPosterVersion() 使版本号变化。所有使用 streamApi.getPosterUrl(id, version)
 * 的组件会自动拿到新 URL（?v=xxx），浏览器视作新请求，强制加载最新海报。
 *
 * 设计要点：
 * 1. 版本号持久化到 localStorage：页面刷新后复用同一版本号，
 *    海报 URL 保持稳定，浏览器可直接命中磁盘缓存（配合后端
 *    Cache-Control: max-age=86400 与 ETag/304，刷新不再全量重新下载）。
 *    此前初始值用 Date.now()，导致每次刷新所有海报都重新加载。
 * 2. bump 时用 Date.now() 而不是 +1，避免并发多次调用被合并为一次自增。
 * 3. 组件中通过 useMediaRefreshStore(s => s.posterVersion) 订阅，获得细粒度更新。
 */
const POSTER_VERSION_KEY = 'nowen:poster-version'

function loadInitialPosterVersion(): number {
  try {
    const stored = Number(localStorage.getItem(POSTER_VERSION_KEY))
    if (Number.isFinite(stored) && stored > 0) return stored
  } catch {
    // localStorage 不可用时退化为会话内时间戳（仅影响跨刷新缓存）
  }
  return Date.now()
}

interface MediaRefreshState {
  posterVersion: number
  bumpPosterVersion: () => void
}

export const useMediaRefreshStore = create<MediaRefreshState>((set) => ({
  posterVersion: loadInitialPosterVersion(),
  bumpPosterVersion: () => {
    const posterVersion = Date.now()
    try {
      localStorage.setItem(POSTER_VERSION_KEY, String(posterVersion))
    } catch {
      // 忽略写入失败：仅损失跨刷新的 URL 稳定性
    }
    set({ posterVersion })
  },
}))

/** 便捷 hook：仅订阅版本号（避免在 selector 中解构整个 state） */
export const usePosterVersion = () => useMediaRefreshStore((s) => s.posterVersion)

/** 非组件环境（如 WS 回调）下直接 bump */
export const bumpPosterVersion = () => useMediaRefreshStore.getState().bumpPosterVersion()
