import api from './client'

// ==================== EMBY 格式兼容导入 ====================

/** EMBY 检测结果 */
export interface EmbyDetectResult {
  is_emby_format: boolean
  folder_type: 'movies' | 'tvshows' | 'mixed' | 'unknown'
  total_files: number
  video_files: number
  nfo_files: number
  image_files: number
  subtitle_files: number
  has_metadata: boolean
  folder_structure: string
  movies: EmbyScannedItem[]
  tvshows: EmbyScannedItem[]
  confidence: number
}

/** EMBY 扫描到的媒体项 */
export interface EmbyScannedItem {
  path: string
  title: string
  year: number
  media_type: 'movie' | 'tvshow'
  video_files: string[]
  nfo_file: string
  poster_file: string
  backdrop_file: string
  subtitle_files: string[]
  seasons?: EmbyScannedSeason[]
  has_nfo: boolean
  imported: boolean
}

/** EMBY 扫描到的季 */
export interface EmbyScannedSeason {
  season_num: number
  path: string
  episodes: number
  video_files: string[]
}

/** EMBY 导入请求 */
export interface EmbyImportRequest {
  root_path: string
  target_library_id: string
  import_mode: 'full' | 'incremental'
  import_nfo: boolean
  import_images: boolean
  import_progress: boolean
  selected_paths?: string[]
}

/** EMBY 导入结果 */
export interface EmbyImportResult {
  total: number
  imported: number
  skipped: number
  failed: number
  movies_imported: number
  series_imported: number
  nfo_parsed: number
  images_mapped: number
  errors: string[]
}

/** EMBY 兼容性信息 */
export interface EmbyCompatInfo {
  supported_nfo_formats: string[]
  supported_image_names: string[]
  supported_folder_structures: string[]
  supported_naming_conventions: string[]
  version: string
}

export const embyCompatApi = {
  /** 检测目录是否为 EMBY 格式 */
  detect: (path: string) =>
    api.get<{ data: EmbyDetectResult }>('/admin/emby/detect', { params: { path } }),

  /** 从 EMBY 格式文件夹导入媒体库 */
  importLibrary: (data: EmbyImportRequest) =>
    api.post<{ message: string; data: EmbyImportResult }>('/admin/emby/import', data),

  /** 获取 EMBY 兼容性信息 */
  getInfo: () =>
    api.get<{ data: EmbyCompatInfo }>('/admin/emby/info'),

  /** 为指定媒体生成 EMBY 兼容 NFO */
  generateNFO: (mediaId: string) =>
    api.get<{ message: string; media_id: string }>(`/admin/emby/nfo/${mediaId}`),
}
