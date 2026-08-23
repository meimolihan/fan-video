import api from './client'
import type {
  Series,
  SeasonInfo,
  Media,
  PaginatedResponse,
  ListResponse,
} from '@/types'

// ==================== 剧集合集 ====================
export const seriesApi = {
  list: (params: { page?: number; size?: number; library_id?: string }) =>
    api.get<PaginatedResponse<Series>>('/series', { params }),

  // 拉取全部剧集（后端 /series 单页上限 100，默认 20；
  // 客户端模式需要完整列表做筛选/统计，这里按页取全）
  listAll: async (params: { library_id?: string } = {}) => {
    const pageSize = 100
    let page = 1
    const all: Series[] = []
    for (;;) {
      const res = await api.get<PaginatedResponse<Series>>('/series', {
        params: { ...params, page, size: pageSize },
      })
      const batch = res.data.data || []
      all.push(...batch)
      if (batch.length === 0 || all.length >= (res.data.total || 0)) break
      page += 1
      if (page > 200) break // 防御：最多 2 万条
    }
    return all
  },

  detail: (id: string) =>
    api.get<{ data: Series }>(`/series/${id}`),

  seasons: (id: string) =>
    api.get<ListResponse<SeasonInfo>>(`/series/${id}/seasons`),

  seasonEpisodes: (id: string, season: number) =>
    api.get<ListResponse<Media>>(`/series/${id}/seasons/${season}`),

  nextEpisode: (id: string, season: number, episode: number) =>
    api.get<{ data: Media | null; message?: string }>(`/series/${id}/next`, {
      params: { season, episode },
    }),

  getPersons: (id: string) =>
    api.get<ListResponse<import('@/types').MediaPerson>>(`/series/${id}/persons`),
}
