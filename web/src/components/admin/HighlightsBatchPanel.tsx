import { useCallback, useEffect, useRef, useState } from 'react'
import { Gauge, RefreshCw, Rocket, Sparkles, Square, Trash2 } from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button, Modal, ModalBody, ModalFooter, ModalHeader } from '@/components/design-system'
import ConfirmDialog from '@/components/design-system/ConfirmDialog'
import { useToast } from '@/components/Toast'
import { mediaAnalysisApi, type BatchHighlightMode, type BatchHighlightStatus, type HighlightAuditItem, type HighlightAuditReport, type HighlightPendingVideo, type HighlightStorageStats } from '@/api/mediaAnalysis'
import { formatErrMsg } from '@/utils/error'
import { invalidateMediaListCaches } from '@/utils/invalidateMediaCaches'

function clampPercent(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

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

export default function HighlightsBatchPanel() {
  const toast = useToast()
  const [status, setStatus] = useState<BatchHighlightStatus | null>(null)
  const [showStartConfirm, setShowStartConfirm] = useState(false)
  const [selectedMode, setSelectedMode] = useState<BatchHighlightMode>('balanced')
  const [showClearConfirm, setShowClearConfirm] = useState(false)
  const [showStopConfirm, setShowStopConfirm] = useState(false)
  const [starting, setStarting] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [stats, setStats] = useState<HighlightStorageStats | null>(null)
  const [showPending, setShowPending] = useState(false)
  const [pendingList, setPendingList] = useState<HighlightPendingVideo[]>([])
  const [pendingLoading, setPendingLoading] = useState(false)
  const [showAudit, setShowAudit] = useState(false)
  const [auditReport, setAuditReport] = useState<HighlightAuditReport | null>(null)
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditCleaning, setAuditCleaning] = useState(false)
  const [includeAssets, setIncludeAssets] = useState(true)
  const pollRef = useRef<number | null>(null)

  // 打开弹窗时实时拉取未处理清单（数据库口径，服务重启后依然准确）
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

  // 完整性检查：源视频缺失 + 片段产物文件缺失
  const openAudit = async () => {
    setShowAudit(true)
    setAuditLoading(true)
    try {
      const response = await mediaAnalysisApi.getHighlightAudit()
      setAuditReport(response.data.data || { total_videos: 0, with_highlights: 0, source_missing: [], assets_missing: [], orphan_caches: [] })
    } catch (error) {
      toast.error(formatErrMsg(error, '完整性检查失败'))
      setShowAudit(false)
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
    } catch (error) {
      toast.error(formatErrMsg(error, '清理失败'))
    } finally {
      setAuditCleaning(false)
    }
  }

  const refreshStats = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getHighlightStorageStats()
      setStats(response.data || null)
    } catch {
      setStats(null)
    }
  }, [])

  const refresh = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getBatchStatus()
      setStatus(response.data.data || null)
      return response.data.data || null
    } catch {
      return null
    }
  }, [])

  useEffect(() => {
    void refresh()
    void refreshStats()
  }, [refresh, refreshStats])

  const running = !!status?.running

  useEffect(() => {
    const delay = running ? 1200 : 5000
    pollRef.current = window.setInterval(() => {
      void refresh()
      void refreshStats()
    }, delay)
    return () => {
      if (pollRef.current) {
        window.clearInterval(pollRef.current)
        pollRef.current = null
      }
    }
  }, [refresh, refreshStats, running])

  const handleStart = async () => {
    setStarting(true)
    try {
      const response = await mediaAnalysisApi.startBatchHighlights(selectedMode)
      setStatus(response.data.data || null)
      toast.success(response.data.message || '批量任务已启动')
      setShowStartConfirm(false)
    } catch (error) {
      toast.error(formatErrMsg(error, '启动批量任务失败'))
    } finally {
      setStarting(false)
    }
  }

  const handleStop = async () => {
    setStopping(true)
    try {
      const response = await mediaAnalysisApi.stopBatchHighlights()
      setStatus(response.data.data || null)
      setShowStopConfirm(false)
      toast.info('已请求停止：剩余视频不再处理，当前视频会正常完成并保留结果')
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
      await refresh()
      await refreshStats()
    } catch (error) {
      toast.error(formatErrMsg(error, '清空精彩片段失败'))
    } finally {
      setClearing(false)
    }
  }

  const total = status?.total || 0
  const processed = status?.processed || 0
  const skipped = status?.skipped || 0
  const failed = status?.failed || 0
  const remaining = status?.remaining ?? 0
  const done = processed + skipped + failed
  const globalPercent = total > 0 ? clampPercent((done / total) * 100) : 0
  const currentTitle = status?.current_title || ''
  const currentPercent = clampPercent(status?.current_progress || 0)

  const coverageTotal = stats?.local_videos ?? 0
  const coverageDone = stats?.highlight_media ?? 0
  const coverageRemaining = Math.max(0, coverageTotal - coverageDone)
  const coveragePercent = coverageTotal > 0 ? clampPercent((coverageDone / coverageTotal) * 100) : 0

  return (
    <>
      <AdminPanel
        title="精彩片段批量处理"
        description="为全部本地视频一键生成精彩片段；已有片段的视频会自动跳过。"
        icon={<Sparkles size={18} />}
        actions={(
          <>
            {/* 刷新：完整性检查 */}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void openAudit()}
              disabled={running}
              aria-label="检查完整性"
              title={running ? '批量运行期间不可用' : '检查已生成片段的完整性（源视频缺失 / 产物文件缺失 / 孤儿缓存目录）'}
            >
              <RefreshCw size={14} className={auditLoading ? 'animate-spin' : undefined} />
              <span className="hidden md:inline">检查完整性</span>
            </Button>
            {running && (
              <Button variant="danger" size="sm" onClick={() => setShowStopConfirm(true)} disabled={stopping}>
                <Square size={14} />
                停止
              </Button>
            )}
            {/* 生成片段：主操作 */}
            <Button variant="primary" size="sm" onClick={() => setShowStartConfirm(true)} disabled={running} loading={starting}>
              <Sparkles size={15} />
              <span className="md:hidden">生成片段</span>
              <span className="hidden md:inline">一键生成精彩片段</span>
            </Button>
            {/* 删除：清空所有片段，仅空闲时可用 */}
            {!running && (
              <Button variant="danger" size="sm" onClick={() => setShowClearConfirm(true)} title="清空所有精彩片段" aria-label="清空所有精彩片段">
                <Trash2 size={14} />
                <span className="hidden md:inline">清空所有精彩片段</span>
              </Button>
            )}
          </>
        )}
      >
        {running ? (
          <div className="space-y-4 px-1 py-2">
            <div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">全局进度</h3>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                  {status?.mode === 'performance' && (
                    <span className="rounded-full border border-[color-mix(in_srgb,var(--nv-accent)_35%,transparent)] bg-[color-mix(in_srgb,var(--nv-accent)_10%,transparent)] px-2 py-0.5 text-xs text-[var(--nv-accent)]">
                      性能模式{status?.parallelism && status.parallelism > 1 ? ` · ${status.parallelism} 并行` : ''}
                    </span>
                  )}
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
                  className={`h-full rounded-full transition-[width] duration-300 ${status?.stop_requested ? 'bg-[var(--nv-status-warning)]' : 'bg-[var(--nv-accent)]'}`}
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
                  {status?.stop_requested && (
                    <span className="rounded-full border border-amber-400/40 bg-amber-400/10 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300">停止中…</span>
                  )}
                </div>
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                  <div className="h-full rounded-full bg-[var(--nv-status-success)] transition-[width] duration-300" style={{ width: `${currentPercent}%` }} />
                </div>
                <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">{Math.round(currentPercent)}%</p>
              </div>
            )}
          </div>
        ) : stats ? (
          <div className="space-y-3 px-1 py-2">
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

            {status?.finished_at && (
              <p className="text-xs text-[var(--nv-text-tertiary)]">
                上次批量完成于 {new Date(status.finished_at).toLocaleString('zh-CN')}：成功 {processed} · 跳过 {skipped}
                {failed > 0 && <> · 失败 {failed}</>}
              </p>
            )}
          </div>
        ) : (
          <p className="px-1 py-2 text-sm text-[var(--nv-text-tertiary)]">
            尚未运行过批量生成。点击「一键生成精彩片段」开始为媒体库中的所有本地视频逐个生成精彩片段。
          </p>
        )}
      </AdminPanel>

      {showStartConfirm && (
        <Modal open size="sm" ariaLabel="选择批量生成模式" onClose={() => { if (!starting) setShowStartConfirm(false) }}>
          <ModalHeader
            title="批量生成精彩片段"
            description={`将为媒体库中的所有本地视频（约 ${stats ? stats.local_videos : total > 0 ? total : '全部'} 个）生成精彩片段。`}
            onClose={() => setShowStartConfirm(false)}
          />
          <ModalBody>
            <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label="生成模式">
              {BATCH_MODES.map((mode) => {
                const active = selectedMode === mode.value
                const Icon = mode.icon
                return (
                  <button
                    key={mode.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    disabled={starting}
                    onClick={() => setSelectedMode(mode.value)}
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
            <p className="mt-3 text-xs leading-5 text-[var(--nv-text-tertiary)]">
              已有精彩片段的视频会自动跳过；如需全部重新生成，请先使用「清空所有精彩片段」。过程耗时较长，可随时停止。
            </p>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowStartConfirm(false)} disabled={starting}>
              取消
            </Button>
            <Button variant="primary" size="sm" onClick={handleStart} loading={starting}>
              开始生成
            </Button>
          </ModalFooter>
        </Modal>
      )}

      {showStopConfirm && (
        <ConfirmDialog
          title="停止批量任务"
          tone="danger"
          description="将停止处理剩余视频。当前正在分析的视频会正常完成并保留结果，之后不再开始新任务。"
          confirmLabel="确认停止"
          onConfirm={handleStop}
          onClose={() => setShowStopConfirm(false)}
          loading={stopping}
        />
      )}

      {showPending && (
        <Modal open size="md" ariaLabel="未处理视频清单" onClose={() => setShowPending(false)}>
          <ModalHeader
            title={`未处理视频（${pendingList.length}）`}
            description="这些视频还没有精彩片段：可能从未生成、上次生成失败或超时，也可能是不支持的格式/文件缺失。重新执行「一键生成」会尝试处理其中受支持的视频。"
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
                    <li
                      key={video.media_id}
                      className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2"
                    >
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
            description="检查已生成片段的完整性：源视频是否仍存在、缩略图/预览文件是否缺失、磁盘缓存目录是否有残留。发现的问题可一键清理，下次「一键生成」会自动补齐。"
            onClose={() => setShowAudit(false)}
          />
          <ModalBody>
            {auditLoading ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">正在扫描全库片段…</p>
            ) : !auditReport ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">暂无检查结果。</p>
            ) : (
              <div className="space-y-4">
                <p className="text-xs leading-5 text-[var(--nv-text-tertiary)]">
                  库内共 {auditReport.total_videos} 个本地视频，其中 {auditReport.with_highlights} 个已生成片段并纳入本次完整性检查。
                  {auditReport.total_videos > auditReport.with_highlights && (
                    <>其余 {auditReport.total_videos - auditReport.with_highlights} 个尚未生成片段，不在本报告范围内，可执行「一键生成」补齐。</>
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
                      ——删除其片段记录后，下次批量生成会自动重新生成完整内容。
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
              <Button
                variant="danger"
                size="sm"
                loading={auditCleaning}
                onClick={() => void handleCleanBroken()}
              >
                清理失效片段
              </Button>
            )}
          </ModalFooter>
        </Modal>
      )}

      {showClearConfirm && (
        <ConfirmDialog
          title="清空所有精彩片段"
          tone="danger"
          description="将删除全库所有视频的精彩片段记录、缩略图与预览文件。源视频不受影响。"
          hint="如需重新生成，可在清空后再次执行「一键生成精彩片段」。此操作不可恢复。"
          confirmLabel="全部清空"
          onConfirm={handleClear}
          onClose={() => setShowClearConfirm(false)}
          loading={clearing}
        />
      )}
    </>
  )
}
