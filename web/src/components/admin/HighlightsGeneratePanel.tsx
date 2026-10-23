import { useCallback, useEffect, useRef, useState } from 'react'
import {
  CheckCircle2,
  FilePlus2,
  FolderSearch,
  Gauge,
  RefreshCw,
  Rocket,
  ShieldCheck,
  Sparkles,
  Square,
  Trash2,
} from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button, Modal, ModalBody, ModalFooter, ModalHeader } from '@/components/design-system'
import ConfirmDialog from '@/components/design-system/ConfirmDialog'
import { useToast } from '@/components/Toast'
import {
  mediaAnalysisApi,
  type BatchHighlightMode,
  type BatchHighlightStatus,
  type HighlightAuditItem,
  type HighlightAuditReport,
  type HighlightPendingVideo,
  type HighlightStorageStats,
  type LocalHighlightGenStatus,
  type LocalHighlightScanReport,
  type ManualHighlightReport,
} from '@/api/mediaAnalysis'
import { formatErrMsg } from '@/utils/error'
import { invalidateMediaListCaches } from '@/utils/invalidateMediaCaches'

type GenerateMethod = 'local' | 'batch'

const METHOD_OPTIONS: Array<{
  value: GenerateMethod
  label: string
  shortLabel: string
  icon: typeof FolderSearch
  desc: string
  detail: string
}> = [
  {
    value: 'local',
    label: '本地精彩片段生成',
    shortLabel: '本地生成',
    icon: FolderSearch,
    desc: '单视频目录增量生成（默认方式）',
    detail: '在视频同目录写入 .highlights 时间线侧车与缩略图，并导入为手动片段；多视频目录会自动跳过。',
  },
  {
    value: 'batch',
    label: '全库批量分析生成',
    shortLabel: '全库分析',
    icon: Sparkles,
    desc: '全库扫描分析，已有片段自动跳过',
    detail: '由分析引擎逐片生成并缓存精彩片段，可选择均衡或性能模式。',
  },
]

const BATCH_MODES: Array<{
  value: BatchHighlightMode
  label: string
  icon: typeof Gauge
  desc: string
  detail: string
}> = [
  {
    value: 'balanced',
    label: '均衡模式',
    icon: Gauge,
    desc: '一次分析一部影片，资源占用低',
    detail: '适合边播放边生成，NAS 友好；整体耗时较长。',
  },
  {
    value: 'performance',
    label: '性能模式',
    icon: Rocket,
    desc: '多部影片并行分析，速度提升 2~3 倍',
    detail: 'CPU 与磁盘占用明显升高，批量期间在线播放可能卡顿。',
  },
]

function clampPercent(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

// 完整性检查报告中的问题分区（纯文本，无图片）
function AuditIssueSection({ title, emptyText, items }: { title: string; emptyText: string; items: HighlightAuditItem[] }) {
  return (
    <div>
      <h4 className={`text-sm font-semibold ${items.length > 0 ? 'text-[var(--nv-status-danger)]' : 'text-[var(--nv-text-secondary)]'}`}>
        {title}
      </h4>
      {items.length === 0 ? (
        <p className="mt-1 text-xs text-[var(--nv-text-tertiary)]">{emptyText}</p>
      ) : (
        <ul className="mt-1.5 max-h-[35vh] space-y-1.5 overflow-y-auto pr-1 sm:max-h-44">
          {items.map((item) => {
            const hasName = !!(item.file || item.title)
            const fileName = item.file || item.title || '(未知文件)'
            return (
              <li key={item.media_id} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2">
                <p className="truncate text-sm font-medium text-[var(--nv-text-primary)]" title={fileName}>
                  {fileName}
                </p>
                {!hasName && (
                  <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">ID: {item.media_id}</p>
                )}
                {item.title && item.file && item.title !== item.file && (
                  <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">{item.title}</p>
                )}
                <p className="mt-0.5 break-all text-xs leading-5 text-[var(--nv-status-danger)]">
                  {item.detail || '完整性异常'}
                  {item.highlights > 0 ? ` · 片段 ${item.highlights} 条` : ''}
                </p>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

// 本地生成目录扫描中的跳过项（多视频目录等）
function LocalSkipSection({ items }: { items: NonNullable<LocalHighlightScanReport['skipped']> }) {
  if (items.length === 0) return null
  return (
    <div>
      <h4 className="text-sm font-semibold text-[var(--nv-status-danger)]">本地生成跳过目录（{items.length}）</h4>
      <ul className="mt-1.5 max-h-[30vh] space-y-1.5 overflow-y-auto pr-1 sm:max-h-44">
        {items.map((item, index) => (
          <li key={index} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2">
            <p className="truncate text-sm font-medium text-[var(--nv-text-primary)]" title={item.dir}>
              {item.dir}
            </p>
            {item.videos.length > 0 && (
              <p className="mt-1 flex flex-wrap gap-1">
                {item.videos.map((v) => (
                  <span key={v} className="rounded border border-[var(--nv-border)] bg-[var(--nv-fill-hover)] px-1.5 py-0.5 text-[11px] text-[var(--nv-text-secondary)]">
                    {v}
                  </span>
                ))}
              </p>
            )}
            <p className="mt-1 break-all text-xs leading-5 text-[var(--nv-status-danger)]">{item.reason}</p>
          </li>
        ))}
      </ul>
    </div>
  )
}

function ResultStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2 text-center">
      <div className="text-lg font-semibold tabular-nums text-[var(--nv-text-primary)]">{value}</div>
      <div className="text-xs text-[var(--nv-text-tertiary)]">{label}</div>
    </div>
  )
}

export default function HighlightsGeneratePanel() {
  const toast = useToast()

  const [method, setMethod] = useState<GenerateMethod>('local')
  const [generateMethod, setGenerateMethod] = useState<GenerateMethod>('local')
  const [batchMode, setBatchMode] = useState<BatchHighlightMode>('balanced')

  const [batchStatus, setBatchStatus] = useState<BatchHighlightStatus | null>(null)
  const [localStatus, setLocalStatus] = useState<LocalHighlightGenStatus | null>(null)
  const [stats, setStats] = useState<HighlightStorageStats | null>(null)
  const [scan, setScan] = useState<LocalHighlightScanReport | null>(null)

  const [showGenerate, setShowGenerate] = useState(false)
  const [starting, setStarting] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [showStopConfirm, setShowStopConfirm] = useState(false)
  const [showClearConfirm, setShowClearConfirm] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

  const [showPending, setShowPending] = useState(false)
  const [pendingList, setPendingList] = useState<HighlightPendingVideo[]>([])
  const [pendingLoading, setPendingLoading] = useState(false)

  const [showAudit, setShowAudit] = useState(false)
  const [auditReport, setAuditReport] = useState<HighlightAuditReport | null>(null)
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditCleaning, setAuditCleaning] = useState(false)
  const [includeAssets, setIncludeAssets] = useState(true)

  const [importReport, setImportReport] = useState<ManualHighlightReport | null>(null)
  const [showImportReport, setShowImportReport] = useState(false)

  const prevRunningRef = useRef(false)

  const refreshBatchStatus = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getBatchStatus()
      setBatchStatus(response.data.data || null)
    } catch {
      setBatchStatus(null)
    }
  }, [])

  const refreshLocalStatus = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getLocalHighlightGenerationStatus()
      setLocalStatus(response.data.data || null)
    } catch {
      setLocalStatus(null)
    }
  }, [])

  const refreshStats = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getHighlightStorageStats()
      setStats(response.data || null)
    } catch {
      setStats(null)
    }
  }, [])

  const refreshScan = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.scanLocalHighlightDirs()
      setScan(response.data.data || null)
    } catch {
      setScan(null)
    }
  }, [])

  useEffect(() => {
    void refreshBatchStatus()
    void refreshLocalStatus()
    void refreshStats()
    void refreshScan()
  }, [refreshBatchStatus, refreshLocalStatus, refreshStats, refreshScan])

  const batchRunning = !!batchStatus?.running
  const localRunning = !!localStatus?.running
  const running = batchRunning || localRunning
  // 运行中时以实际任务为准，空闲时以当前所选方式为准
  const viewMethod: GenerateMethod = batchRunning ? 'batch' : localRunning ? 'local' : method

  // 任务结束或首次挂载：补齐最新扫描与统计
  useEffect(() => {
    if (prevRunningRef.current && !running) {
      void refreshBatchStatus()
      void refreshLocalStatus()
      void refreshStats()
      void refreshScan()
    }
    prevRunningRef.current = running
  }, [running, refreshBatchStatus, refreshLocalStatus, refreshStats, refreshScan])

  // 统一轮询：运行中加快，空闲时低频刷新
  useEffect(() => {
    const delay = running ? 1400 : 5000
    const id = window.setInterval(() => {
      void refreshBatchStatus()
      void refreshLocalStatus()
      void refreshStats()
    }, delay)
    return () => window.clearInterval(id)
  }, [running, refreshBatchStatus, refreshLocalStatus, refreshStats])

  const openGenerate = () => {
    setGenerateMethod(method)
    setShowGenerate(true)
  }

  const handleGenerate = async () => {
    setStarting(true)
    try {
      if (generateMethod === 'local') {
        const notGenerated = (scan?.eligible || []).filter((item) => !item.generated).map((item) => item.media_id)
        const response = await mediaAnalysisApi.generateLocalHighlights({
          media_ids: notGenerated.length ? notGenerated : undefined,
        })
        setLocalStatus(response.data.data || null)
        toast.success(response.data.message || '本地精彩片段生成任务已启动')
      } else {
        const response = await mediaAnalysisApi.startBatchHighlights(batchMode)
        setBatchStatus(response.data.data || null)
        toast.success(response.data.message || '批量任务已启动')
      }
      setMethod(generateMethod)
      setShowGenerate(false)
      void refreshScan()
    } catch (error) {
      toast.error(formatErrMsg(error, generateMethod === 'local' ? '启动本地精彩片段生成失败' : '启动批量任务失败'))
    } finally {
      setStarting(false)
    }
  }

  const handleStop = async () => {
    setStopping(true)
    try {
      if (batchRunning) {
        const response = await mediaAnalysisApi.stopBatchHighlights()
        setBatchStatus(response.data.data || null)
        toast.info('已请求停止：剩余视频不再处理，当前视频会正常完成并保留结果')
      } else {
        const response = await mediaAnalysisApi.stopLocalHighlightGeneration()
        setLocalStatus(response.data.data || null)
        toast.info('已请求停止：剩余目录不再处理，已生成的保留结果')
      }
      setShowStopConfirm(false)
    } catch (error) {
      toast.error(formatErrMsg(error, '停止失败'))
    } finally {
      setStopping(false)
      void refreshStats()
    }
  }

  const handleClear = async () => {
    setClearing(true)
    try {
      const response = await mediaAnalysisApi.clearAllHighlights()
      toast.success(`已清空 ${response.data.highlight_count} 个精彩片段（涉及 ${response.data.media_count} 个视频）`)
      invalidateMediaListCaches()
      setShowClearConfirm(false)
      await refreshBatchStatus()
      await refreshStats()
      await refreshScan()
    } catch (error) {
      toast.error(formatErrMsg(error, '清空精彩片段失败'))
    } finally {
      setClearing(false)
    }
  }

  const handleDeleteLocal = async () => {
    setDeleting(true)
    try {
      const response = await mediaAnalysisApi.cleanupLocalHighlights()
      toast.success(response.data.message || `已删除 ${response.data.data.files_deleted} 个文件`)
      invalidateMediaListCaches()
      setShowDeleteConfirm(false)
      await refreshLocalStatus()
      await refreshScan()
      await refreshStats()
    } catch (error) {
      toast.error(formatErrMsg(error, '删除本地精彩片段失败'))
    } finally {
      setDeleting(false)
    }
  }

  const handleVerify = async () => {
    setVerifying(true)
    try {
      const response = await mediaAnalysisApi.verifyLocalHighlights()
      const report = response.data.data
      invalidateMediaListCaches()
      await refreshScan()
      toast.success(`校验完成：检查 ${report.total_checked} 个，修复 ${report.repaired} 个，补记 ${report.completed} 个`)
    } catch (error) {
      toast.error(formatErrMsg(error, '校验本地精彩片段失败'))
    } finally {
      setVerifying(false)
    }
  }

  const handleImportManual = async () => {
    const response = await mediaAnalysisApi.importManualHighlights()
    setImportReport(response.data.data || null)
    toast.success(response.data.message || '手动片段导入完成')
    invalidateMediaListCaches()
    setShowImportReport(true)
  }

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await refreshLocalStatus()
      await handleImportManual()
      await refreshLocalStatus()
      await refreshScan()
    } catch (error) {
      toast.error(formatErrMsg(error, '刷新目录并导入手动片段失败'))
    } finally {
      setRefreshing(false)
    }
  }

  const openPending = async () => {
    setShowPending(true)
    setPendingLoading(true)
    try {
      const response = await mediaAnalysisApi.getPendingHighlightVideos()
      setPendingList(response.data.data || [])
    } catch {
      setPendingList([])
    } finally {
      setPendingLoading(false)
    }
  }

  // 合并两种方式的检查：批量完整性报告 + 本地目录扫描问题
  const openAudit = async () => {
    setShowAudit(true)
    setAuditLoading(true)
    try {
      const [auditResult, scanResult] = await Promise.allSettled([
        mediaAnalysisApi.getHighlightAudit(),
        mediaAnalysisApi.scanLocalHighlightDirs(),
      ])
      if (auditResult.status === 'fulfilled') {
        setAuditReport(
          auditResult.value.data.data || {
            total_videos: 0,
            with_highlights: 0,
            source_missing: [],
            assets_missing: [],
            orphan_caches: [],
          },
        )
      } else {
        setAuditReport(null)
        toast.error(formatErrMsg(auditResult.reason, '完整性检查失败'))
        setShowAudit(false)
      }
      if (scanResult.status === 'fulfilled') {
        setScan(scanResult.value.data.data || null)
      }
    } finally {
      setAuditLoading(false)
    }
  }

  const handleCleanBroken = async () => {
    if (!auditReport) return
    setAuditCleaning(true)
    try {
      const response = await mediaAnalysisApi.cleanBrokenHighlights(includeAssets)
      toast.success(response.data.message || '清理完成')
      setShowAudit(false)
      await refreshStats()
      await refreshScan()
    } catch (error) {
      toast.error(formatErrMsg(error, '清理失败'))
    } finally {
      setAuditCleaning(false)
    }
  }

  const total = batchStatus?.total || 0
  const processed = batchStatus?.processed || 0
  const skipped = batchStatus?.skipped || 0
  const failed = batchStatus?.failed || 0
  const remaining = batchStatus?.remaining ?? 0
  const done = processed + skipped + failed
  const globalPercent = total > 0 ? clampPercent((done / total) * 100) : 0
  const currentTitle = batchStatus?.current_title || ''
  const currentPercent = clampPercent(batchStatus?.current_progress || 0)

  const localTotal = localStatus?.total || 0
  const localRemaining = localStatus?.remaining ?? 0
  const localGlobalPercent = localTotal > 0 ? clampPercent(((localTotal - localRemaining) / localTotal) * 100) : 0
  const localCurrentPercent =
    localStatus && (localStatus.current_like || 0) > 0
      ? clampPercent(((localStatus.current_done || 0) / localStatus.current_like) * 100)
      : 0

  const coverageTotal = stats?.local_videos ?? 0
  const coverageDone = stats?.highlight_media ?? 0
  const coverageRemaining = Math.max(0, coverageTotal - coverageDone)
  const coveragePercent = coverageTotal > 0 ? clampPercent((coverageDone / coverageTotal) * 100) : 0

  const eligible = scan?.eligible || []
  const skippedList = scan?.skipped || []

  const showLocalActions = !running && viewMethod === 'local'
  const showBatchActions = !running && viewMethod === 'batch'

  return (
    <>
      <AdminPanel
        title="精彩片段批量生成"
        description="为媒体库视频批量生成精彩片段。可选择「本地精彩片段生成」（默认，增量写入视频目录）或「全库批量分析生成」；两种任务互斥，已有片段的视频会自动跳过。"
        icon={<Sparkles size={18} />}
        actions={(
          <>
            {running && (
              <Button variant="danger" size="sm" onClick={() => setShowStopConfirm(true)} loading={stopping}>
                <Square size={14} />
                停止
              </Button>
            )}
            {showLocalActions && (
              <>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void handleRefresh()}
                  loading={refreshing}
                  aria-label="刷新目录并导入手动片段"
                  title="刷新目录扫描，并将各视频同目录的 .highlights 手工片段导入为手动精彩片段"
                >
                  <RefreshCw size={14} className={refreshing ? 'animate-spin' : undefined} />
                  <span className="hidden md:inline">刷新</span>
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => void handleVerify()}
                  loading={verifying}
                  title="对照磁盘产物校验已生成状态：修复「已生成但产物被删」和「有产物未记已生成」两类不一致，不触发探测、速度很快"
                  aria-label="校验本地精彩片段一致性"
                >
                  <ShieldCheck size={14} />
                  <span className="hidden md:inline">校验</span>
                </Button>
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => setShowDeleteConfirm(true)}
                  disabled={starting}
                  title="删除本地生成的 .highlights 文件并清除对应手动精彩片段记录"
                  aria-label="删除本地生成的精彩片段"
                >
                  <Trash2 size={14} />
                  <span className="hidden md:inline">删除本地</span>
                </Button>
              </>
            )}
            {!running && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => void openAudit()}
                aria-label="检查完整性"
                title="检查已生成片段的完整性（源视频缺失 / 产物文件缺失 / 孤儿缓存目录 / 本地目录扫描）"
              >
                <RefreshCw size={14} className={auditLoading ? 'animate-spin' : undefined} />
                <span className="hidden md:inline">检查完整性</span>
              </Button>
            )}
            {showBatchActions && (
              <Button variant="danger" size="sm" onClick={() => setShowClearConfirm(true)} title="清空所有精彩片段" aria-label="清空所有精彩片段">
                <Trash2 size={14} />
                <span className="hidden md:inline">清空所有精彩片段</span>
              </Button>
            )}
            <Button variant="primary" size="sm" onClick={openGenerate} disabled={running} loading={starting}>
              <FilePlus2 size={15} />
              <span className="md:hidden">生成片段</span>
              <span className="hidden md:inline">生成精彩片段</span>
            </Button>
          </>
        )}
      >
        {running ? (
          <div className="space-y-4 px-1 py-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="rounded-full border border-[color-mix(in_srgb,var(--nv-accent)_35%,transparent)] bg-[color-mix(in_srgb,var(--nv-accent)_10%,transparent)] px-2 py-0.5 text-xs text-[var(--nv-accent)]">
                {viewMethod === 'local' ? '本地精彩片段生成 · 运行中' : '全库批量分析生成 · 运行中'}
              </span>
              {viewMethod === 'batch' && batchStatus?.mode === 'performance' && (
                <span className="rounded-full border border-[var(--nv-border)] px-2 py-0.5 text-xs text-[var(--nv-text-tertiary)]">
                  性能模式{batchStatus?.parallelism && batchStatus.parallelism > 1 ? ` · ${batchStatus.parallelism} 并行` : ''}
                </span>
              )}
            </div>

            {viewMethod === 'local' ? (
              <>
                <div>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">生成进度</h3>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                      <span className="whitespace-nowrap">待处理 <b className="text-[var(--nv-text-primary)]">{localRemaining}</b></span>
                      <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{localStatus?.processed || 0}</b></span>
                      {(localStatus?.already || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">无变动 {localStatus?.already}</span>}
                      {(localStatus?.skipped || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">跳过 {localStatus?.skipped}</span>}
                      {(localStatus?.failed || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-status-danger)]">失败 {localStatus?.failed}</span>}
                    </div>
                  </div>
                  <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                    <div
                      className={`h-full rounded-full transition-[width] duration-300 ${localStatus?.stop_requested ? 'bg-[var(--nv-status-warning)]' : 'bg-[var(--nv-accent)]'}`}
                      style={{ width: `${localGlobalPercent}%` }}
                    />
                  </div>
                  <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">{localGlobalPercent.toFixed(1)}%</p>
                </div>

                {localStatus?.current_video && (
                  <div>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <h3 className="min-w-0 max-w-full truncate pr-2 text-sm font-semibold text-[var(--nv-text-primary)]" title={localStatus.current_dir}>
                        当前生成：<span className="font-normal text-[var(--nv-text-secondary)]">{localStatus.current_video}</span>
                      </h3>
                      {localStatus.stop_requested && (
                        <span className="rounded-full border border-amber-400/40 bg-amber-400/10 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300">停止中…</span>
                      )}
                    </div>
                    <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                      <div className="h-full rounded-full bg-[var(--nv-status-success)] transition-[width] duration-300" style={{ width: `${localCurrentPercent}%` }} />
                    </div>
                    <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">{Math.round(localCurrentPercent)}%</p>
                  </div>
                )}
              </>
            ) : (
              <>
                <div>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">全局进度</h3>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                      <span className="whitespace-nowrap">视频总数 <b className="text-[var(--nv-text-primary)]">{total}</b></span>
                      <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{processed}</b></span>
                      <span className="whitespace-nowrap">未处理 <b className="text-[var(--nv-text-primary)]">{remaining}</b></span>
                      {(skipped > 0 || failed > 0) && (
                        <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">
                          跳过 {skipped}
                          {failed > 0 && <> · 失败 {failed}</>}
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                    <div
                      className={`h-full rounded-full transition-[width] duration-300 ${batchStatus?.stop_requested ? 'bg-[var(--nv-status-warning)]' : 'bg-[var(--nv-accent)]'}`}
                      style={{ width: `${globalPercent}%` }}
                    />
                  </div>
                  <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">{globalPercent.toFixed(1)}%</p>
                </div>

                {currentTitle && (
                  <div>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <h3 className="min-w-0 max-w-full truncate pr-2 text-sm font-semibold text-[var(--nv-text-primary)]" title={currentTitle}>
                        当前处理：<span className="font-normal text-[var(--nv-text-secondary)]">{currentTitle}</span>
                      </h3>
                      {batchStatus?.stop_requested && (
                        <span className="rounded-full border border-amber-400/40 bg-amber-400/10 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300">停止中…</span>
                      )}
                    </div>
                    <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                      <div className="h-full rounded-full bg-[var(--nv-status-success)] transition-[width] duration-300" style={{ width: `${currentPercent}%` }} />
                    </div>
                    <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">{Math.round(currentPercent)}%</p>
                  </div>
                )}
              </>
            )}
          </div>
        ) : (
          <div className="space-y-4 px-1 py-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-[var(--nv-text-tertiary)]">生成方式</span>
              <div
                className="inline-flex rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] p-0.5"
                role="tablist"
                aria-label="生成方式"
              >
                {METHOD_OPTIONS.map((option) => {
                  const active = method === option.value
                  const Icon = option.icon
                  return (
                    <button
                      key={option.value}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      onClick={() => setMethod(option.value)}
                      className={`flex items-center gap-1.5 rounded-[calc(var(--nv-radius-control)-2px)] px-2.5 py-1 text-xs transition-colors ${
                        active
                          ? 'bg-[color-mix(in_srgb,var(--nv-accent)_14%,transparent)] text-[var(--nv-accent)]'
                          : 'text-[var(--nv-text-secondary)] hover:bg-[var(--nv-fill-hover)]'
                      }`}
                    >
                      <Icon size={13} />
                      {option.shortLabel}
                    </button>
                  )
                })}
              </div>
              <span className="text-xs text-[var(--nv-text-tertiary)]">
                默认本地生成；切换方式后操作按钮会随之变化。
              </span>
            </div>

            {/* 统一覆盖概览（两种方式共用，默认展示） */}
            {stats ? (
              <div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">库内覆盖情况</h3>
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                    <span className="whitespace-nowrap">视频总数 <b className="text-[var(--nv-text-primary)]">{stats.local_videos}</b></span>
                    <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{stats.highlight_media}</b></span>
                    <span className="whitespace-nowrap">
                      未处理{' '}
                      {coverageRemaining > 0 ? (
                        <button
                          type="button"
                          onClick={() => void openPending()}
                          className="font-medium text-[var(--nv-status-danger)] hover:underline"
                        >
                          {coverageRemaining}
                        </button>
                      ) : (
                        <b className="text-[var(--nv-text-primary)]">{coverageRemaining}</b>
                      )}
                    </span>
                  </div>
                </div>
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                  <div className="h-full rounded-full bg-[var(--nv-accent)] transition-[width] duration-300" style={{ width: `${coveragePercent}%` }} />
                </div>
                <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">
                  片段共 {stats.highlight_rows} 条 · 覆盖 {coveragePercent.toFixed(1)}%
                </p>
              </div>
            ) : (
              <p className="text-sm text-[var(--nv-text-tertiary)]">正在读取库内片段统计…</p>
            )}

            {method === 'local' ? (
              <div className="space-y-3 border-t border-[var(--nv-border)] pt-3">
                <div>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">本地生成状态</h3>
                    <span className="text-xs text-[var(--nv-text-tertiary)]">
                      {localStatus?.finished_at
                        ? '上次生成完成于 ' + new Date(localStatus.finished_at).toLocaleString('zh-CN')
                        : '尚未运行过本地生成'}
                    </span>
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                    <span>成功 <b className="text-[var(--nv-status-success)]">{localStatus?.processed ?? 0}</b></span>
                    <span>无变动 <b className="text-[var(--nv-text-tertiary)]">{localStatus?.already ?? 0}</b></span>
                    <span>跳过 <b className="text-[var(--nv-text-tertiary)]">{localStatus?.skipped ?? 0}</b></span>
                    <span>失败 <b className={(localStatus?.failed ?? 0) > 0 ? 'text-[var(--nv-status-danger)]' : 'text-[var(--nv-text-tertiary)]'}>{localStatus?.failed ?? 0}</b></span>
                  </div>
                  {localStatus && localStatus.failed > 0 && localStatus.last_error && (
                    <p className="mt-1 rounded-[var(--nv-radius-control)] border border-[var(--nv-status-danger)]/40 bg-[var(--nv-status-danger)]/5 px-3 py-2 text-xs leading-5 text-[var(--nv-status-danger)]">
                      上次失败原因：{localStatus.last_error}
                    </p>
                  )}
                </div>

                {scan ? (
                  <>
                    <div>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <h4 className="text-sm font-semibold text-[var(--nv-text-primary)]">目录概况</h4>
                        <span className="rounded-full border border-[var(--nv-border)] px-2 py-0.5 text-[11px] text-[var(--nv-text-tertiary)]">
                          {scan.full_scan ? '全量扫描（逐片探测）' : '增量扫描（无探测）'}
                        </span>
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                        <span className="whitespace-nowrap">可生成 <b className="text-[var(--nv-text-primary)]">{scan.eligible_dirs}</b></span>
                        <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{scan.already_dirs}</b></span>
                        <span className="whitespace-nowrap">待处理 <b className="text-[var(--nv-text-primary)]">{scan.eligible_dirs - scan.already_dirs}</b></span>
                        <span className="whitespace-nowrap">跳过 <b className={scan.skipped_dirs > 0 ? 'text-[var(--nv-status-danger)]' : 'text-[var(--nv-text-tertiary)]'}>{scan.skipped_dirs}</b></span>
                      </div>
                    </div>
                    <div className="h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                      <div
                        className="h-full rounded-full bg-[var(--nv-accent)] transition-[width] duration-300"
                        style={{ width: `${scan.eligible_dirs > 0 ? clampPercent((scan.already_dirs / scan.eligible_dirs) * 100) : 0}%` }}
                      />
                    </div>

                    {eligible.length > 0 && (
                      <div>
                        <h4 className="flex items-center gap-1.5 text-sm font-semibold text-[var(--nv-text-primary)]">
                          <CheckCircle2 size={14} className="text-[var(--nv-status-success)]" />
                          符合条件（单视频目录 · {eligible.length}）
                        </h4>
                        <ul className="mt-1.5 max-h-[30vh] space-y-1.5 overflow-y-auto pr-1 sm:max-h-56">
                          {eligible.map((item) => (
                            <li key={item.dir} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2">
                              <div className="flex items-center gap-2">
                                <p className="min-w-0 flex-1 truncate text-sm font-medium text-[var(--nv-text-primary)]" title={item.dir}>
                                  {item.title}
                                </p>
                                {item.generated ? (
                                  <span className="shrink-0 rounded-full border border-[color-mix(in_srgb,var(--nv-accent)_35%,transparent)] bg-[color-mix(in_srgb,var(--nv-accent)_10%,transparent)] px-2 py-0.5 text-[11px] text-[var(--nv-accent)]">
                                    已生成
                                  </span>
                                ) : item.status === 'failed' ? (
                                  <span className="shrink-0 rounded-full border border-[var(--nv-status-danger)]/40 bg-[var(--nv-status-danger)]/10 px-2 py-0.5 text-[11px] text-[var(--nv-status-danger)]">
                                    失败可重试
                                  </span>
                                ) : (
                                  <span className="shrink-0 rounded-full border border-[color-mix(in_srgb,var(--nv-status-success)_35%,transparent)] bg-[color-mix(in_srgb,var(--nv-status-success)_10%,transparent)] px-2 py-0.5 text-[11px] text-[var(--nv-status-success)]">
                                    待生成
                                  </span>
                                )}
                              </div>
                              <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">
                                {item.video_file}
                                {item.duration > 0
                                  ? ` · 时长 ${Math.round(item.duration / 60)} 分钟 · 计划 ${item.clips} 段`
                                  : ' · 时长未探测（生成时自动分析）'}
                              </p>
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}

                    <LocalSkipSection items={skippedList} />

                    {(eligible.length === 0 && skippedList.length === 0) ? (
                      <p className="text-sm text-[var(--nv-text-tertiary)]">媒体库中尚未发现视频目录。</p>
                    ) : (
                      <p className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2 text-xs leading-5 text-[var(--nv-text-tertiary)]">
                        增量模式：只处理待生成/失败的影片，已生成的旧影片不会再被探测。同目录多个视频会跳过（将视频移入独立目录即可）；「校验」可修复产物被删后的状态不一致。
                      </p>
                    )}
                  </>
                ) : (
                  <p className="text-sm text-[var(--nv-text-tertiary)]">正在扫描媒体库目录…</p>
                )}
              </div>
            ) : (
              <div className="border-t border-[var(--nv-border)] pt-3">
                {batchStatus?.finished_at ? (
                  <p className="text-xs text-[var(--nv-text-tertiary)]">
                    上次全库批量完成于 {new Date(batchStatus.finished_at).toLocaleString('zh-CN')}：成功 {processed} · 跳过 {skipped}
                    {failed > 0 && <> · 失败 {failed}</>}
                  </p>
                ) : (
                  <p className="text-xs text-[var(--nv-text-tertiary)]">
                    尚未运行过全库批量生成。已有精彩片段的视频会自动跳过；如需全部重新生成，请先使用「清空所有精彩片段」。
                  </p>
                )}
              </div>
            )}
          </div>
        )}
      </AdminPanel>

      {showGenerate && (
        <Modal open size="md" ariaLabel="选择生成方式" onClose={() => { if (!starting) setShowGenerate(false) }}>
          <ModalHeader
            title="生成精彩片段"
            description={
              generateMethod === 'local'
                ? '将为符合条件（单视频目录）且尚未生成的影片生成时间线片段与缩略图。'
                : `将为媒体库中的所有本地视频（约 ${stats ? stats.local_videos : total > 0 ? total : '全部'} 个）分析生成精彩片段。`
            }
            onClose={() => setShowGenerate(false)}
          />
          <ModalBody>
            <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label="生成方式">
              {METHOD_OPTIONS.map((option) => {
                const active = generateMethod === option.value
                const Icon = option.icon
                return (
                  <button
                    key={option.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    disabled={starting}
                    onClick={() => setGenerateMethod(option.value)}
                    className={`rounded-[var(--nv-radius-control)] border p-3 text-left transition-colors ${
                      active
                        ? 'border-[var(--nv-accent)] bg-[color-mix(in_srgb,var(--nv-accent)_10%,transparent)]'
                        : 'border-[var(--nv-border)] hover:bg-[var(--nv-fill-hover)]'
                    }`}
                  >
                    <div className="flex items-center gap-2">
                      <Icon size={16} className={active ? 'text-[var(--nv-accent)]' : 'text-[var(--nv-text-tertiary)]'} />
                      <span className="text-sm font-medium text-[var(--nv-text-primary)]">{option.label}</span>
                      {active && <span className="ml-auto h-2 w-2 rounded-full bg-[var(--nv-accent)]" aria-hidden="true" />}
                    </div>
                    <p className="mt-1.5 text-xs text-[var(--nv-text-secondary)]">{option.desc}</p>
                    <p className="mt-1 text-xs leading-4 text-[var(--nv-text-tertiary)]">{option.detail}</p>
                  </button>
                )
              })}
            </div>

            {generateMethod === 'batch' && (
              <div className="mt-3">
                <h4 className="text-xs font-medium text-[var(--nv-text-secondary)]">分析模式</h4>
                <div className="mt-1.5 grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label="分析模式">
                  {BATCH_MODES.map((mode) => {
                    const active = batchMode === mode.value
                    const Icon = mode.icon
                    return (
                      <button
                        key={mode.value}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        disabled={starting}
                        onClick={() => setBatchMode(mode.value)}
                        className={`rounded-[var(--nv-radius-control)] border p-3 text-left transition-colors ${
                          active
                            ? 'border-[var(--nv-accent)] bg-[color-mix(in_srgb,var(--nv-accent)_10%,transparent)]'
                            : 'border-[var(--nv-border)] hover:bg-[var(--nv-fill-hover)]'
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          <Icon size={16} className={active ? 'text-[var(--nv-accent)]' : 'text-[var(--nv-text-tertiary)]'} />
                          <span className="text-sm font-medium text-[var(--nv-text-primary)]">{mode.label}</span>
                          {active && <span className="ml-auto h-2 w-2 rounded-full bg-[var(--nv-accent)]" aria-hidden="true" />}
                        </div>
                        <p className="mt-1.5 text-xs text-[var(--nv-text-secondary)]">{mode.desc}</p>
                        <p className="mt-1 text-xs leading-4 text-[var(--nv-text-tertiary)]">{mode.detail}</p>
                      </button>
                    )
                  })}
                </div>
              </div>
            )}

            <p className="mt-3 text-xs leading-5 text-[var(--nv-text-tertiary)]">
              {generateMethod === 'local'
                ? '默认增量处理待生成/失败的影片；多视频目录会跳过。生成物写入视频目录 .highlights/ 并自动导入。'
                : '已有精彩片段的视频会自动跳过；过程耗时较长，可随时停止。'}
            </p>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowGenerate(false)} disabled={starting}>
              取消
            </Button>
            <Button variant="primary" size="sm" onClick={handleGenerate} loading={starting}>
              开始生成
            </Button>
          </ModalFooter>
        </Modal>
      )}

      {showStopConfirm && (
        <ConfirmDialog
          title="停止生成任务"
          tone="danger"
          description={
            batchRunning
              ? '将停止处理剩余视频。当前正在分析的视频会正常完成并保留结果，之后不再开始新任务。'
              : '将停止处理剩余目录。当前正在生成的视频会正常完成并保留结果，之后不再处理新目录。'
          }
          confirmLabel="确认停止"
          onConfirm={handleStop}
          onClose={() => setShowStopConfirm(false)}
          loading={stopping}
        />
      )}

      {showClearConfirm && (
        <ConfirmDialog
          title="清空所有精彩片段"
          tone="danger"
          description="将删除全库所有视频的精彩片段记录、缩略图与预览文件。源视频不受影响。"
          hint="如需重新生成，可在清空后再次执行「生成精彩片段」。此操作不可恢复。"
          confirmLabel="全部清空"
          onConfirm={handleClear}
          onClose={() => setShowClearConfirm(false)}
          loading={clearing}
        />
      )}

      {showDeleteConfirm && (
        <ConfirmDialog
          title="删除本地生成的精彩片段"
          tone="danger"
          description="将删除各单视频目录下由本地生成方式写入的 .highlights 侧车文件（json / 缩略图 / 旧的切割片段），并清除对应的「手动」精彩片段记录。源视频不受影响。"
          hint="仅处理单视频目录下的生成物；手工导入的其它片段不受影响。此操作不可恢复。"
          confirmLabel="删除"
          onConfirm={handleDeleteLocal}
          onClose={() => setShowDeleteConfirm(false)}
          loading={deleting}
        />
      )}

      {showPending && (
        <Modal open size="md" ariaLabel="未处理视频清单" onClose={() => setShowPending(false)}>
          <ModalHeader
            title={`未处理视频（${pendingList.length}）`}
            description="这些视频还没有精彩片段：可能从未生成、上次生成失败或超时，也可能是不支持的格式/文件缺失。重新执行生成会尝试处理其中受支持的视频。"
            onClose={() => setShowPending(false)}
          />
          <ModalBody>
            {pendingLoading ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">加载中…</p>
            ) : pendingList.length === 0 ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">没有未处理的视频。</p>
            ) : (
              <ul className="max-h-[45vh] space-y-2 overflow-y-auto pr-1 sm:max-h-80">
                {pendingList.map((video) => {
                  const fileName = video.file || video.title
                  return (
                    <li key={video.media_id} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2">
                      <p className="truncate text-sm font-medium text-[var(--nv-text-primary)]" title={fileName}>
                        {fileName}
                      </p>
                      {video.title && video.file && video.title !== video.file && (
                        <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]" title={video.title}>
                          {video.title}
                        </p>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowPending(false)}>
              关闭
            </Button>
          </ModalFooter>
        </Modal>
      )}

      {showAudit && (
        <Modal open size="md" ariaLabel="片段完整性检查" onClose={() => { if (!auditCleaning) setShowAudit(false) }}>
          <ModalHeader
            title="片段完整性检查"
            description="合并检查两种生成方式：源视频是否仍存在、缩略图/预览产物是否缺失、缓存目录是否有残留，以及本地生成方式下的多视频目录问题。"
            onClose={() => setShowAudit(false)}
          />
          <ModalBody>
            {auditLoading ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">正在扫描全库片段与本地目录…</p>
            ) : !auditReport ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">暂无检查结果。</p>
            ) : (
              <div className="space-y-4">
                <p className="text-xs leading-5 text-[var(--nv-text-tertiary)]">
                  库内共 {auditReport.total_videos} 个本地视频，其中 {auditReport.with_highlights} 个已生成片段并纳入本次完整性检查。
                  {auditReport.total_videos > auditReport.with_highlights && (
                    <>其余 {auditReport.total_videos - auditReport.with_highlights} 个尚未生成片段，不在本报告范围内，可执行「生成精彩片段」补齐。</>
                  )}
                </p>

                <AuditIssueSection
                  title={`源视频已删除（${auditReport.source_missing.length}）`}
                  emptyText="没有源视频缺失的问题。"
                  items={auditReport.source_missing}
                />
                <AuditIssueSection
                  title={`片段文件缺失（${auditReport.assets_missing.length}）`}
                  emptyText="没有产物文件缺失的问题。"
                  items={auditReport.assets_missing}
                />
                <AuditIssueSection
                  title={`孤儿缓存目录（${auditReport.orphan_caches.length}）`}
                  emptyText="没有孤儿缓存目录。"
                  items={auditReport.orphan_caches}
                />

                <LocalSkipSection items={skippedList} />

                {(auditReport.source_missing.length > 0 || auditReport.orphan_caches.length > 0 || auditReport.assets_missing.length > 0) && (
                  <label className="flex cursor-pointer items-start gap-2 rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] p-3">
                    <input
                      type="checkbox"
                      checked={includeAssets}
                      onChange={(e) => setIncludeAssets(e.target.checked)}
                      className="mt-0.5 h-4 w-4 accent-[var(--nv-accent)]"
                      disabled={auditCleaning}
                    />
                    <span className="text-xs leading-5 text-[var(--nv-text-secondary)]">
                      同时清理「片段文件缺失」项（推荐）
                      ——删除其片段记录后，下次生成会自动重新生成完整内容。
                    </span>
                  </label>
                )}
              </div>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowAudit(false)} disabled={auditCleaning}>
              关闭
            </Button>
            {auditReport && (auditReport.source_missing.length > 0 || auditReport.orphan_caches.length > 0 || (includeAssets && auditReport.assets_missing.length > 0)) && (
              <Button variant="danger" size="sm" loading={auditCleaning} onClick={() => void handleCleanBroken()}>
                清理失效片段
              </Button>
            )}
          </ModalFooter>
        </Modal>
      )}

      {showImportReport && importReport && (
        <Modal open size="md" ariaLabel="手动片段导入结果" onClose={() => setShowImportReport(false)}>
          <ModalHeader
            title="手动片段导入完成"
            description="已扫描各视频同目录的 .highlights/ 目录，将手工 ffmpeg 剪辑的片段导入为「手动」精彩片段。重复导入会整体刷新手动片段，但不会覆盖自动生成结果。"
            onClose={() => setShowImportReport(false)}
          />
          <ModalBody>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <ResultStat label="目录数" value={importReport.dirs_found} />
              <ResultStat label="涉及视频" value={importReport.media_scanned} />
              <ResultStat label="导入片段" value={importReport.clips_imported} />
              <ResultStat label="跳过片段" value={importReport.clips_skipped} />
            </div>
            {importReport.skipped && importReport.skipped.length > 0 && (
              <div className="mt-4">
                <h4 className="text-sm font-semibold text-[var(--nv-status-warning)]">跳过的片段（无法归属到视频）</h4>
                <ul className="mt-1.5 max-h-44 space-y-1.5 overflow-y-auto pr-1">
                  {importReport.skipped.map((item, index) => (
                    <li key={index} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-1.5 text-xs">
                      <p className="break-all font-medium text-[var(--nv-text-primary)]" title={item.path}>{item.path}</p>
                      <p className="mt-0.5 text-[var(--nv-text-tertiary)]">{item.reason}</p>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowImportReport(false)}>关闭</Button>
          </ModalFooter>
        </Modal>
      )}
    </>
  )
}
