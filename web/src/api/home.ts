import api from './client'
import type { MixedItem } from '@/types'

export interface HomeFeaturedEntry {
  id: string
  item_type: 'movie' | 'series'
  item_id: string
  /** movie | episode | series，用于展示精确类型 */
  kind?: 'movie' | 'episode' | 'series'
  title?: string
  year?: number
  valid: boolean
  created_at: string
}

export interface HomeFeaturedListResult {
  data: MixedItem[]
  active: boolean
  min_items: number
}

// ==================== 首页手动精选轮播 ====================
export const homeApi = {
  // 首页：条目数 >= min_items 时返回精选列表，否则空数组（前端回落默认逻辑）
  getFeaturedCarousel: () => api.get<HomeFeaturedListResult>('/home/featured'),

  // 管理端
  listFeatured: () =>
    api.get<{ data: HomeFeaturedEntry[]; min_items: number }>('/admin/home-featured'),

  addFeatured: (itemType: 'movie' | 'series', itemId: string) =>
    api.post<{ data: HomeFeaturedEntry; min_items: number; total: number; active: boolean }>(
      '/admin/home-featured',
      { item_type: itemType, item_id: itemId },
    ),

  removeFeatured: (id: string) =>
    api.delete<{ data: { id: string }; min_items: number; total: number; active: boolean }>(
      `/admin/home-featured/${id}`,
    ),
}
