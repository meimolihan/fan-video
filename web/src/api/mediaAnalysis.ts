import api from './client'

export const MEDIA_COMPUTE_PROTOCOL_VERSION = 2
export const MEDIA_COMPUTE_JOB_HIGHLIGHT_V1 = 'highlight_v1'
export const MEDIA_COMPUTE_CAPABILITY_HIGHLIGHT_V1 = 'highlight_v1'
export const MEDIA_COMPUTE_JOB_PREVIEW_THUMBNAIL_V1 = 'preview_thumbnail_v1'
export const MEDIA_COMPUTE_CAPABILITY_PREVIEW_THUMBNAIL_V1 = 'preview_thumbnail_v1'
export const MEDIA_COMPUTE_JOB_CHAPTER_DETECT_V1 = 'chapter_detect_v1'
export const MEDIA_COMPUTE_CAPABILITY_CHAPTER_DETECT_V1 = 'chapter_detect_v1'

export interface MediaHighlight {
  id: string
  media_id: string
  title: string
  start_time: number
  end_time: number
  score: number
  tags: string
  source: string
  analysis_method: string
  thumbnail_url?: string
  preview_url?: string
  version: number
}

export interface MediaHighlightList {
  highlights: MediaHighlight[]
  stale: boolean
}

export interface HighlightExport {
  highlight_id: string
  media_id: string
  title: string
  file_name: string
  size_bytes: number
  duration: number
  exported_at: string
}

export interface MediaAnalysisTask {
  id: string
  media_id: string
  task_type: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'interrupted' | string
  stage: string
  progress: number
  result?: string
  error?: string
  started_at?: string | null
  completed_at?: string | null
  created_at?: string
  updated_at?: string
}

export type MediaAnalysisExecutionMode = 'auto' | 'client_preferred' | 'server_only' | 'off'

export type BatchHighlightMode = 'balanced' | 'performance'

export interface BatchHighlightStatus {
  running: boolean
  mode?: BatchHighlightMode | string
  parallelism?: number
  stop_requested: boolean
  total: number
  processed: number
  skipped: number
  failed: number
  remaining: number
  current_media_id: string
  current_title: string
  current_progress: number
  started_at: string
  finished_at?: string | null
}

export interface MediaAnalysisWorkerConfig {
  execution_mode: MediaAnalysisExecutionMode
  modes: MediaAnalysisExecutionMode[]
}

export interface MediaAnalysisWorkerHeartbeat {
  worker_id: string
  kind: 'android' | 'desktop' | 'client' | string
  name: string
  version: string
  capabilities: string[]
  network: string
  charging: boolean
  battery_percent: number
}

export interface MediaAnalysisWorker extends MediaAnalysisWorkerHeartbeat {
  client_protocol_version?: number
  last_seen: string
  state: 'idle' | 'busy' | 'unavailable' | string
  task_id?: string
  current_job_type?: string
}

export interface MediaComputeHighlightInput {
  media_id: string
  fingerprint: string
  duration: number
  stream_url: string
  sample_times: number[]
  max_highlights: number
  engine_version: number
}

export interface MediaComputePreviewThumbnailInput {
  media_id: string
  highlight_id: string
  fingerprint: string
  stream_url: string
  frame_times: number[]
  max_width: number
  frame_rate: number
}

export interface MediaComputePreviewFrame {
  time: number
  mime: string
  data_base64: string
}

export interface MediaComputePreviewThumbnailResult {
  fingerprint: string
  highlight_id: string
  frames: MediaComputePreviewFrame[]
}

export interface MediaComputeChapterDetectInput {
  media_id: string
  fingerprint: string
  duration: number
  stream_url: string
  sample_times: number[]
  probe_gap_seconds: number
  min_chapter_seconds: number
  max_chapters: number
  capture_width: number
  engine_version: number
}

export interface MediaComputeChapterCandidate {
  time: number
  score: number
}

export interface MediaComputeChapterDetectResult {
  fingerprint: string
  candidates: MediaComputeChapterCandidate[]
}

export interface MediaComputeTaskClaim<TInput = unknown> {
  protocol_version?: number
  job_type?: string
  required_capability?: string
  task_id: string
  claim_token: string
  input?: TInput
  lease_expires_at?: string

  // V1 compatibility mirror. Only highlight_v1 should ever populate these.
  media_id?: string
  fingerprint?: string
  duration?: number
  stream_url?: string
  sample_times?: number[]
  max_highlights?: number
  engine_version?: number
}

export type MediaAnalysisWorkerClaim = MediaComputeTaskClaim<MediaComputeHighlightInput>

export interface MediaAnalysisWorkerProgress {
  claim_token: string
  stage: string
  progress: number
}

export interface MediaAnalysisWorkerResultItem {
  title?: string
  start_time: number
  end_time: number
  score: number
  analysis_method: string
  thumbnail_base64?: string
  thumbnail_mime?: string
}

export interface MediaAnalysisWorkerComplete {
  claim_token: string
  fingerprint: string
  highlights: MediaAnalysisWorkerResultItem[]
}

export interface MediaComputeHighlightResult {
  fingerprint: string
  highlights: MediaAnalysisWorkerResultItem[]
}

export interface MediaComputeTaskComplete<TResult = unknown> {
  claim_token: string
  job_type: string
  result: TResult
}

export interface MediaAnalysisWorkerFailure {
  claim_token: string
  error: string
}

export interface HighlightStorageStats {
  highlight_rows: number
  highlight_media: number
  local_videos: number
  highlight_tasks: number
}

// 尚未生成精彩片段的本地视频（覆盖缺口明细，实时取自数据库）
export interface HighlightPendingVideo {
  media_id: string
  title: string
  file: string
}

// 完整性检查发现的问题媒体
export interface HighlightAuditItem {
  media_id: string
  title?: string
  file?: string
  highlights: number
  detail?: string
}

export interface HighlightAuditReport {
  total_videos: number
  with_highlights: number
  source_missing: HighlightAuditItem[]
  assets_missing: HighlightAuditItem[]
  orphan_caches: HighlightAuditItem[]
}

export const mediaAnalysisApi = {
  getHighlights: (mediaId: string) =>
    api.get<{ data: MediaHighlightList }>(`/media/${mediaId}/highlights`),

  analyzeHighlights: (mediaId: string) =>
    api.post<{ data: MediaAnalysisTask; message: string; execution_mode?: MediaAnalysisExecutionMode }>(`/media/${mediaId}/highlights/analyze`),

  getStatus: (mediaId: string) =>
    api.get<{ data: MediaAnalysisTask | null }>(`/media/${mediaId}/highlights/status`),

  deleteHighlights: (mediaId: string) =>
    api.delete<{ message: string }>(`/media/${mediaId}/highlights`),

  // ==================== 批量处理（媒体库管理） ====================
  startBatchHighlights: (mode: BatchHighlightMode = 'balanced') =>
    api.post<{ data: BatchHighlightStatus; message: string }>('/admin/media-analysis/batch', { mode }),

  getBatchStatus: () =>
    api.get<{ data: BatchHighlightStatus }>('/admin/media-analysis/batch/status'),

  stopBatchHighlights: () =>
    api.delete<{ data: BatchHighlightStatus; message: string }>('/admin/media-analysis/batch'),

  clearAllHighlights: () =>
    api.delete<{ message: string; media_count: number; highlight_count: number }>('/admin/media-analysis/highlights-all'),

  getHighlightStorageStats: () =>
    api.get<HighlightStorageStats>('/admin/media-analysis/highlights-stats'),

  // 未生成精彩片段的视频清单（覆盖口径：本地视频 - 已有片段媒体）
  getPendingHighlightVideos: () =>
    api.get<{ data: HighlightPendingVideo[] }>('/admin/media-analysis/highlights-pending'),

  // 片段完整性检查（源视频缺失 / 缩略图预览产物缺失）
  getHighlightAudit: () =>
    api.get<{ data: HighlightAuditReport }>('/admin/media-analysis/highlights-audit'),

  // 清理检查出的问题片段；includeAssetIssues 同时清理产物缺失项
  cleanBrokenHighlights: (includeAssetIssues: boolean) =>
    api.post<{ cleaned: number; message: string }>('/admin/media-analysis/highlights-audit/clean', {
      include_asset_issues: includeAssetIssues,
    }),

  // 导出精彩片段为独立 mp4（同步切片，短片段秒级完成）
  exportHighlight: (mediaId: string, highlightId: string) =>
    api.post<{ data: HighlightExport; message: string }>(`/media/${mediaId}/highlights/${highlightId}/export`),

  listHighlightExports: (mediaId: string) =>
    api.get<{ data: HighlightExport[] }>(`/media/${mediaId}/highlights/exports`),

  deleteHighlightExport: (mediaId: string, highlightId: string) =>
    api.delete<{ message: string }>(`/media/${mediaId}/highlights/${highlightId}/export`),

  getWorkerConfig: () =>
    api.get<{ data: MediaAnalysisWorkerConfig }>('/admin/media-analysis/config'),

  updateWorkerConfig: (executionMode: MediaAnalysisExecutionMode) =>
    api.put<{ data: Pick<MediaAnalysisWorkerConfig, 'execution_mode'> }>('/admin/media-analysis/config', {
      execution_mode: executionMode,
    }),

  getWorkers: () =>
    api.get<{ data: MediaAnalysisWorker[] }>('/admin/media-analysis/workers'),

  heartbeatWorker: (heartbeat: MediaAnalysisWorkerHeartbeat) =>
    api.post<{ data: MediaAnalysisWorker }>('/media-analysis/workers/heartbeat', heartbeat),

  // Historical transport URL, V2 generic claim envelope.
  claimWorkerTask: (heartbeat: MediaAnalysisWorkerHeartbeat) =>
    api.post<{ data?: MediaComputeTaskClaim }>('/media-analysis/workers/claim', heartbeat),

  updateWorkerProgress: (taskId: string, progress: MediaAnalysisWorkerProgress) =>
    api.post<void>(`/media-analysis/workers/tasks/${taskId}/progress`, progress),

  // V1 compatibility call kept for already-shipped clients and any old web bundle.
  completeWorkerTask: (taskId: string, result: MediaAnalysisWorkerComplete) =>
    api.post<{ message: string }>(`/media-analysis/workers/tasks/${taskId}/complete`, result),

  // V2 uses the same transport path but sends a job-scoped generic result envelope.
  completeComputeTask: <TResult>(taskId: string, result: MediaComputeTaskComplete<TResult>) =>
    api.post<{ message: string }>(`/media-analysis/workers/tasks/${taskId}/complete`, result),

  failWorkerTask: (taskId: string, failure: MediaAnalysisWorkerFailure) =>
    api.post<void>(`/media-analysis/workers/tasks/${taskId}/fail`, failure),
}
