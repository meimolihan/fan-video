import api from './client'
import type {
  User,
  SystemInfo,
  ListResponse,
  UserPermission,
  UpdatePermissionRequest,
  ContentRating,
  SystemSettings,
  LoginLog,
  AuditLog,
  InviteCode,
} from '@/types'

// ==================== 管理 ====================
export const adminApi = {
  listUsers: () =>
    api.get<ListResponse<User>>('/admin/users'),

  createUser: (data: { username: string; password: string; role?: 'admin' | 'user'; nickname?: string; email?: string }) =>
    api.post<{ data: User }>('/admin/users', data),

  updateUser: (id: string, data: { role?: 'admin' | 'user'; nickname?: string; email?: string; avatar?: string }) =>
    api.put<{ data: User }>(`/admin/users/${id}`, data),

  setUserDisabled: (id: string, disabled: boolean) =>
    api.post<{ message: string }>(`/admin/users/${id}/disabled`, { disabled }),

  deleteUser: (id: string) =>
    api.delete(`/admin/users/${id}`),

  resetUserPassword: (id: string, newPassword: string, forceChangeOnNextLogin: boolean = true) =>
    api.put<{ message: string }>(`/admin/users/${id}/password`, {
      new_password: newPassword,
      force_change_on_next_login: forceChangeOnNextLogin,
    }),

  // 登录日志 & 审计日志
  listLoginLogs: (params?: { page?: number; size?: number; only_failed?: boolean }) =>
    api.get<ListResponse<LoginLog>>('/admin/login-logs', { params }),

  listAuditLogs: (params?: { page?: number; size?: number; action?: string }) =>
    api.get<ListResponse<AuditLog>>('/admin/audit-logs', { params }),

  // 邀请码
  listInviteCodes: () =>
    api.get<ListResponse<InviteCode>>('/admin/invite-codes'),

  createInviteCode: (data: { code?: string; max_uses?: number; expires_in_hours?: number; note?: string }) =>
    api.post<{ data: InviteCode }>('/admin/invite-codes', data),

  deleteInviteCode: (id: string) =>
    api.delete(`/admin/invite-codes/${id}`),

  systemInfo: () =>
    api.get<{ data: SystemInfo }>('/admin/system'),

  // 批量操作
  batchScan: (libraryIds: string[]) =>
    api.post('/admin/batch/scan', { library_ids: libraryIds }),

  batchScrape: (mediaIds: string[]) =>
    api.post('/admin/batch/scrape', { media_ids: mediaIds }),

  // 权限管理
  getUserPermission: (userId: string) =>
    api.get<{ data: UserPermission }>(`/admin/permissions/${userId}`),

  updateUserPermission: (userId: string, data: UpdatePermissionRequest) =>
    api.put(`/admin/permissions/${userId}`, data),

  // 内容分级
  getContentRating: (mediaId: string) =>
    api.get<{ data: ContentRating }>(`/admin/rating/${mediaId}`),

  setContentRating: (mediaId: string, level: string) =>
    api.put(`/admin/rating/${mediaId}`, { level }),

  deleteMedia: (mediaId: string) =>
    api.delete(`/admin/media/${mediaId}`),

  updateMediaMetadata: (mediaId: string, data: {
    title?: string
    orig_title?: string
    year?: number
    overview?: string
    rating?: number
    genres?: string
    country?: string
    language?: string
    tagline?: string
    studio?: string
  }) =>
    api.put<{ message: string; data: import('@/types').Media }>(`/admin/media/${mediaId}/metadata`, data),

  // 剧集合集管理
  scrapeSeriesMetadata: (seriesId: string) =>
    api.post(`/admin/series/${seriesId}/scrape`),

  deleteSeries: (seriesId: string) =>
    api.delete(`/admin/series/${seriesId}`),

  updateSeriesMetadata: (seriesId: string, data: {
    title?: string
    orig_title?: string
    year?: number
    overview?: string
    rating?: number
    genres?: string
    country?: string
    language?: string
    studio?: string
  }) =>
    api.put<{ message: string; data: import('@/types').Series }>(`/admin/series/${seriesId}/metadata`, data),

  // 系统全局设置
  getSystemSettings: () =>
    api.get<{ data: SystemSettings }>('/admin/settings/system'),

  updateSystemSettings: (data: Partial<SystemSettings>) =>
    api.put<{ data: SystemSettings }>('/admin/settings/system', data),

  // 图片管理
  uploadMediaImage: (mediaId: string, file: File, imageType: 'poster' | 'backdrop' = 'poster') => {
    const formData = new FormData()
    formData.append('file', file)
    return api.post(`/admin/media/${mediaId}/image/upload?type=${imageType}`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  uploadSeriesImage: (seriesId: string, file: File, imageType: 'poster' | 'backdrop' = 'poster') => {
    const formData = new FormData()
    formData.append('file', file)
    return api.post(`/admin/series/${seriesId}/image/upload?type=${imageType}`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  setMediaImageByURL: (mediaId: string, url: string, imageType: 'poster' | 'backdrop' = 'poster') =>
    api.post(`/admin/media/${mediaId}/image/url`, { url, image_type: imageType }),

  setSeriesImageByURL: (seriesId: string, url: string, imageType: 'poster' | 'backdrop' = 'poster') =>
    api.post(`/admin/series/${seriesId}/image/url`, { url, image_type: imageType }),

  // 文件系统浏览
  browseFS: (path: string) =>
    api.get<{ data: { current: string; parent: string; items: { name: string; path: string; is_dir: boolean }[] } }>('/admin/fs/browse', {
      params: { path },
    }),

  // 一键清空数据（保留影视文件）
  clearAllData: (confirm: string) =>
    api.post<{ data: {
      status: string
      message: string
      total_cleared: number
      success_count: number
      error_count: number
      details: { table: string; cleared: number; status: string; message?: string }[]
    } }>('/admin/system/clear-data', { confirm }),

  // 剧集合并（多季自动合并为一个整体）
  mergeSeries: (primaryId: string, secondaryIds: string[]) =>
    api.post<{ message: string; data: {
      primary_series_id: string
      primary_title: string
      merged_count: number
      total_episodes: number
      total_seasons: number
      merged_series_ids: string[]
    } }>('/admin/series/merge', { primary_id: primaryId, secondary_ids: secondaryIds }),

  autoMergeSeries: () =>
    api.post<{ message: string; data: {
      groups_processed: number
      total_merged: number
      details: {
        primary_series_id: string
        primary_title: string
        merged_count: number
        total_episodes: number
        total_seasons: number
        merged_series_ids: string[]
      }[]
    } }>('/admin/series/auto-merge'),

  mergeCandidates: () =>
    api.get<{ data: {
      normalized_title: string
      count: number
      series: { id: string; title: string; season_count: number; episode_count: number; poster_path: string }[]
    }[]; total: number }>('/admin/series/merge-candidates'),

  // 重复媒体检测
  detectDuplicates: (libraryId?: string) =>
    libraryId
      ? api.get<{ data: import('@/types').DuplicateGroup[]; total: number }>(`/admin/libraries/${libraryId}/duplicates`)
      : api.get<{ data: import('@/types').DuplicateGroup[]; total: number }>('/admin/duplicates'),

  markDuplicates: (libraryId: string) =>
    api.post<{ message: string; marked: number }>(`/admin/libraries/${libraryId}/mark-duplicates`),

  // 手动预处理单个媒体（用户显式意图，带 force=true 以绕过"可直接播放则跳过"的自动判定）
  submitPreprocess: (mediaId: string) =>
    api.post<{ message: string }>('/admin/preprocess/submit', { media_id: mediaId, force: true }),

  // 手动转码单个媒体（通过预处理提交）
  submitTranscode: (mediaId: string) =>
    api.post<{ message: string }>('/admin/preprocess/submit', { media_id: mediaId, force: true }),
}
