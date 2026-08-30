import api from './client'

// ==================== 批量元数据编辑 ====================
export const batchMetadataApi = {
  // 批量更新媒体元数据
  batchUpdateMedia: (data: import('@/types').BatchUpdateRequest) =>
    api.post<{ message: string; data: import('@/types').BatchUpdateResult }>('/admin/batch/metadata/media', data),

  // 批量更新剧集合集元数据
  batchUpdateSeries: (data: { series_ids: string[]; updates: Record<string, string> }) =>
    api.post<{ message: string; data: import('@/types').BatchUpdateResult }>('/admin/batch/metadata/series', data),
}

// ==================== 媒体库导出 / 本地数据导入 ====================
export const importExportApi = {
  // 导出媒体库数据
  exportLibrary: (libraryId?: string) =>
    api.get<{ message: string; data: import('@/types').ExportData }>('/admin/export/library', { params: { library_id: libraryId } }),

  // 从导出数据导入
  importFromData: (data: { data: import('@/types').ExportData; target_library_id: string }) =>
    api.post<{ message: string; data: import('@/types').ImportResult }>('/admin/import/data', data),
}

// ==================== 全量备份 / 还原 ====================

export type SystemBackupEntry = {
  name: string
  size: number
  created_at: string
  version?: string
}

export type RestoreResult = {
  pre_backup_name: string
  restored_config: number
  restored_data: number
  restored_resources: number
  staged_database: boolean
  restart_required: boolean
  warnings?: string[]
}

export const systemBackupApi = {
  // 备份列表
  list: () =>
    api.get<{ data: { backups: SystemBackupEntry[]; backup_dir: string } }>('/admin/backups'),

  // 创建全量备份
  create: () =>
    api.post<{ message: string; data: SystemBackupEntry }>('/admin/backups', undefined, { timeout: 0 }),

  // 下载备份到本地
  download: (name: string) =>
    api.get(`/admin/backups/${encodeURIComponent(name)}/download`, { responseType: 'blob' }),

  // 删除备份
  remove: (name: string) =>
    api.delete<{ message: string }>(`/admin/backups/${encodeURIComponent(name)}`),

  // 上传备份并还原（需携带强确认字符串）
  restore: (file: File, confirm: string) => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('confirm', confirm)
    return api.post<{ message: string; data: RestoreResult }>('/admin/backups/restore', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 0,
    })
  },

  // 直接还原服务器上已有的备份文件（需携带强确认字符串）
  restoreLocal: (name: string, confirm: string) =>
    api.post<{ message: string; data: RestoreResult }>(`/admin/backups/${encodeURIComponent(name)}/restore`, { confirm }, { timeout: 0 }),
}
