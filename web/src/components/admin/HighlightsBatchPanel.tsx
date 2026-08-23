import { useCallback, useEffect, useRef, useState } from 'react'
import { Sparkles, Square, Trash2 } from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button } from '@/components/design-system'
import ConfirmDialog from '@/components/design-system/ConfirmDialog'
import { useToast } from '@/components/Toast'
import { mediaAnalysisApi, type BatchHighlightStatus, type HighlightStorageStats } from '@/api/mediaAnalysis'
import { formatErrMsg } from '@/utils/error'
import { invalidateMediaListCaches } from '@/utils/invalidateMediaCaches'

function clampPercent(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

export default function HighlightsBatchPanel() {
  const toast = useToast()
  const [status, setStatus] = useState<BatchHighlightStatus | null>(null)
  const [showStartConfirm, setShowStartConfirm] = useState(false)
  const [showClearConfirm, setShowClearConfirm] = useState(false)
  const [showStopConfirm, setShowStopConfirm] = useState(false)
  const [starting, setStarting] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [stats, setStats] = useState<HighlightStorageStats | null>(null)
  const pollRef = useRef<number | null>(null)

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
      const response = await mediaAnalysisApi.startBatchHighlights()
      setStatus(response.data.data || null)
      toast.success(response.data.message || '批量任务已启动')
      setShowStartConfirm(false)
    } catch (error: any) {
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
      toast.info('已请求停止，剩余视频不再处理；当前视频完成后其结果将被丢弃')
    } catch (error: any) {
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
    } catch (error: any) {
      toast.error(formatErrMsg(error, '清空精彩片段失败'))
    } finally {
      setClearing(false)
    }
  }

  const total = status?.total || 0
  const processed = status?.processed || 0
  const skipped = status?.skipped || 0
  const discarded = status?.discarded || 0
  const failed = status?.failed || 0
  const remaining = status?.remaining ?? 0
  const done = processed + skipped + discarded + failed
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
            {running ? (
              <Button variant="danger" size="sm" onClick={() => setShowStopConfirm(true)} disabled={stopping}>
                <Square size={14} />
                停止
              </Button>
            ) : (
              <Button variant="danger" size="sm" onClick={() => setShowClearConfirm(true)}>
                <Trash2 size={14} />
                清空所有精彩片段
              </Button>
            )}
            <Button variant="primary" size="sm" onClick={() => setShowStartConfirm(true)} disabled={running} loading={starting}>
              <Sparkles size={15} />
              一键生成精彩片段
            </Button>
          </>
        )}
      >
        {running ? (
          <div className="space-y-4 px-1 py-2">
            <div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">全局进度</h3>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--nv-text-secondary)]">
                  <span>视频总数 <b className="text-[var(--nv-text-primary)]">{total}</b></span>
                  <span>已生成 <b className="text-[var(--nv-status-success)]">{processed}</b></span>
                  <span>未处理 <b className="text-[var(--nv-text-primary)]">{remaining}</b></span>
                  {(skipped > 0 || discarded > 0 || failed > 0) && (
                    <span className="text-[var(--nv-text-tertiary)]">
                      跳过 {skipped}
                      {discarded > 0 && <> · 放弃 {discarded}</>}
                      {failed > 0 && <> · 失败 {failed}</>}
                    </span>
                  )}
                </div>
              </div>
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-surface-elevated)]">
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
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-surface-elevated)]">
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
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--nv-text-secondary)]">
                  <span>视频总数 <b className="text-[var(--nv-text-primary)]">{stats.local_videos}</b></span>
                  <span>已生成 <b className="text-[var(--nv-status-success)]">{stats.highlight_media}</b></span>
                  <span>未处理 <b className="text-[var(--nv-text-primary)]">{coverageRemaining}</b></span>
                </div>
              </div>
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-surface-elevated)]">
                <div className="h-full rounded-full bg-[var(--nv-accent)] transition-[width] duration-300" style={{ width: `${coveragePercent}%` }} />
              </div>
              <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">
                片段共 {stats.highlight_rows} 条 · 覆盖 {coveragePercent.toFixed(1)}%
              </p>
            </div>

            {status?.finished_at && (
              <p className="text-xs text-[var(--nv-text-tertiary)]">
                上次批量完成于 {new Date(status.finished_at).toLocaleString('zh-CN')}：成功 {processed} · 跳过 {skipped}
                {discarded > 0 && <> · 放弃（已删除）{discarded}</>}
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
        <ConfirmDialog
          title="批量生成精彩片段"
          description={`将为媒体库中的所有本地视频（约 ${stats ? stats.local_videos : total > 0 ? total : '全部'} 个）逐个分析并生成精彩片段。`}
          hint="已有精彩片段的视频会自动跳过；如需全部重新生成，请先使用「清空所有精彩片段」。过程耗时较长，可随时停止。"
          confirmLabel="开始生成"
          onConfirm={handleStart}
          onClose={() => setShowStartConfirm(false)}
          loading={starting}
        />
      )}

      {showStopConfirm && (
        <ConfirmDialog
          title="停止批量任务"
          tone="danger"
          description="将停止处理剩余视频。已生成完毕的片段会保留；当前正在分析的视频不会保留结果。"
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
