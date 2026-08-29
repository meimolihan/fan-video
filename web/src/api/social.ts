import api from './client'
import type {
  Bookmark,
  CreateBookmarkRequest,
  ListResponse,
  PaginatedResponse,
} from '@/types'

// ==================== 视频书签 ====================

export const bookmarkApi = {
  create: (data: CreateBookmarkRequest) =>
    api.post<{ data: Bookmark }>('/bookmarks', data),

  listByUser: (page = 1, size = 20) =>
    api.get<PaginatedResponse<Bookmark>>('/bookmarks', { params: { page, size } }),

  listByMedia: (mediaId: string) =>
    api.get<ListResponse<Bookmark>>(`/bookmarks/media/${mediaId}`),

  update: (id: string, title: string, note: string) =>
    api.put(`/bookmarks/${id}`, { title, note }),

  delete: (id: string) =>
    api.delete(`/bookmarks/${id}`),
}

// ==================== 评论 ====================
import type {
  Comment,
  CreateCommentRequest,
  CommentListResponse,
} from '@/types'

export const commentApi = {
  listByMedia: (mediaId: string, page = 1, size = 20) =>
    api.get<CommentListResponse>(`/media/${mediaId}/comments`, { params: { page, size } }),

  create: (mediaId: string, data: CreateCommentRequest) =>
    api.post<{ data: Comment }>(`/media/${mediaId}/comments`, data),

  delete: (id: string) =>
    api.delete(`/comments/${id}`),
}

// ==================== 播放统计 ====================
export const statsApi = {
  recordPlayback: (mediaId: string, watchMinutes: number) =>
    api.post('/stats/playback', { media_id: mediaId, watch_minutes: watchMinutes }),

  getMyStats: () =>
    api.get<{ data: import('@/types').UserStatsOverview }>('/stats/me'),

  clearMyStats: () =>
    api.delete<{ message?: string; deleted?: number }>('/stats/me'),
}
