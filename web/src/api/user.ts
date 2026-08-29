import api from './client'
import type {
  User,
  WatchHistory,
  Favorite,
  WatchLaterItem,
  PaginatedResponse,
  LoginLog,
} from '@/types'
import { toAbsolutePlaybackPosition } from '@/playback/sessionRuntime'
import { invalidatePageCachePrefix } from '@/hooks/usePageCache'

type FavoriteMutationState = {
  revision: number
  favorited: boolean
}

let favoriteMutationRevision = 0
const favoriteMutationState = new Map<string, FavoriteMutationState>()

function publishFavoriteChanged(mediaId: string, favorited: boolean) {
  // 记录本窗口最近一次已确认的收藏变更。checkFavorite 可能比收藏写请求更早发出、
  // 却更晚返回；通过 revision 可以阻止旧检查结果把刚刚更新的 UI 状态覆盖掉。
  favoriteMutationRevision += 1
  favoriteMutationState.set(mediaId, { revision: favoriteMutationRevision, favorited })

  // 收藏列表使用 usePageCache。收藏状态变化后必须立即让所有分页缓存失效，
  // 否则从详情页返回/进入“我的收藏”时会继续命中 15 秒旧缓存，看起来像必须刷新页面才生效。
  invalidatePageCachePrefix('favorites:')

  // 同步通知当前窗口内可能仍挂载的收藏相关视图。后续其它组件接入收藏数量/
  // 快捷收藏时也可以直接复用，不需要各自猜测缓存键。
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('nowen:favorites-updated', {
      detail: { mediaId, favorited },
    }))
  }
}

// ==================== 稍后再看（Watch Later） ====================
// 与收藏一致的“就近写 + 事件广播”模式，保证卡片/详情页/列表页状态始终一致，
// 并让稍后再看列表缓存保持最新。
type WatchLaterMutationState = {
  revision: number
  added: boolean
}

let watchLaterMutationRevision = 0
const watchLaterMutationState = new Map<string, WatchLaterMutationState>()

function publishWatchLaterChanged(mediaId: string, added: boolean) {
  watchLaterMutationRevision += 1
  watchLaterMutationState.set(mediaId, { revision: watchLaterMutationRevision, added })

  invalidatePageCachePrefix('watch-later:')
  invalidatePageCachePrefix('media-detail:')

  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('nowen:watch-later-updated', {
      detail: { mediaId, added },
    }))
  }
}

// ==================== 用户 ====================
export const userApi = {
  profile: () =>
    api.get<{ data: User }>('/users/me'),

  updateProfile: (data: { username?: string; nickname?: string; email?: string; avatar?: string }) =>
    api.put<{ data: User; token?: string; expires_at?: number }>('/users/me', data),

  loginLogs: () =>
    api.get<{ data: LoginLog[] }>('/users/me/login-logs'),

  updateProgress: (mediaId: string, position: number, duration: number) =>
    api.put(`/users/me/progress/${mediaId}`, {
      position: toAbsolutePlaybackPosition(mediaId, position),
      duration,
    }),

  favorites: (page = 1, size = 20) =>
    api.get<PaginatedResponse<Favorite>>('/users/me/favorites', { params: { page, size } }),

  addFavorite: async (mediaId: string) => {
    const response = await api.post(`/users/me/favorites/${mediaId}`)
    publishFavoriteChanged(mediaId, true)
    return response
  },

  removeFavorite: async (mediaId: string) => {
    const response = await api.delete(`/users/me/favorites/${mediaId}`)
    publishFavoriteChanged(mediaId, false)
    return response
  },

  clearFavorites: async () => {
    const response = await api.delete<{ message?: string; deleted?: number }>('/users/me/favorites')
    // 清空是全量删除：把本窗口记录过的所有已收藏条目广播为未收藏，
    // 让仍挂载的心形按钮（详情页/卡片）同步熄灭，并失效收藏列表缓存。
    for (const [mediaId, state] of favoriteMutationState) {
      if (state.favorited) publishFavoriteChanged(mediaId, false)
    }
    return response
  },

  checkFavorite: async (mediaId: string) => {
    const startedRevision = favoriteMutationState.get(mediaId)?.revision ?? 0
    const response = await api.get<{ data: boolean }>(`/users/me/favorites/${mediaId}/check`)
    const latestMutation = favoriteMutationState.get(mediaId)

    // 如果这次检查请求发出后用户完成了一次收藏/取消收藏，那么服务端检查响应
    // 可能已经是旧快照。保持最近一次已确认写操作的状态，避免心形按钮“点亮后又熄灭”。
    if (latestMutation && latestMutation.revision !== startedRevision) {
      response.data.data = latestMutation.favorited
    }
    return response
  },

  getProgress: (mediaId: string) =>
    api.get<{ data: import('@/types').WatchHistory | null }>(`/users/me/progress/${mediaId}`),

  // ==================== 稍后再看 ====================
  watchLater: (page = 1, size = 20) =>
    api.get<PaginatedResponse<WatchLaterItem>>('/users/me/watch-later', { params: { page, size } }),

  addWatchLater: async (mediaId: string) => {
    const response = await api.post(`/users/me/watch-later/${mediaId}`)
    publishWatchLaterChanged(mediaId, true)
    return response
  },

  removeWatchLater: async (mediaId: string) => {
    const response = await api.delete(`/users/me/watch-later/${mediaId}`)
    publishWatchLaterChanged(mediaId, false)
    return response
  },

  clearWatchLater: async () => {
    const response = await api.delete<{ deleted?: number }>('/users/me/watch-later')
    for (const [mediaId, state] of watchLaterMutationState) {
      if (state.added) publishWatchLaterChanged(mediaId, false)
    }
    return response
  },

  checkWatchLater: async (mediaId: string) => {
    const startedRevision = watchLaterMutationState.get(mediaId)?.revision ?? 0
    const response = await api.get<{ data: boolean }>(`/users/me/watch-later/${mediaId}/check`)
    const latestMutation = watchLaterMutationState.get(mediaId)
    if (latestMutation && latestMutation.revision !== startedRevision) {
      response.data.data = latestMutation.added
    }
    return response
  },

  history: (page = 1, size = 20) =>
    api.get<PaginatedResponse<WatchHistory>>('/users/me/history', { params: { page, size } }),

  deleteHistory: (mediaId: string) =>
    api.delete(`/users/me/history/${mediaId}`),

  clearHistory: () =>
    api.delete('/users/me/history'),
}
