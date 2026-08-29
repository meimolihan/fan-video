import { useCallback, useEffect, useRef, useState } from 'react'
import { Image, RefreshCw, Square, Sparkles, Trash2 } from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button, Modal, ModalBody, ModalFooter, ModalHeader } from '@/components/design-system'
import ConfirmDialog from '@/components/design-system/ConfirmDialog'
import { useToast } from '@/components/Toast'
import { mediaAnalysisApi, type ThumbnailAuditItem, type ThumbnailAuditReport, type ThumbnailBatchStatus, type ThumbnailStats } from '@/api/mediaAnalysis'
import { formatErrMsg } from '@/utils/error'

function clampPercent(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

// 完整性检查报告中的问题分区（与 HighlightsBatchPanel 模式一致）
function AuditIssueSection({ title, emptyText, items }: { title: string; emptyText: string; items: ThumbnailAuditItem[] }) {
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
            const displayName = item.title || item.media_id || '(未知文件)'
            return (
              <li key={item.media_id || item.thumb_path} className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2">
                <p className="truncate text-sm font-medium text-[var(--nv-text-primary)]" title={displayName}>
                  {displayName}
                </p>
                {item.title && item.media_id && item.title !== item.media_id && (
                  <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">ID: {item.media_id}</p>
                )}
                <p className="mt-0.5 break-all text-xs leading-5 text-[var(--nv-status-danger)]">
                  {item.detail || '完整性异常'}
                </p>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

export default function ThumbnailManagementPanel() {
  const toast = useToast()
  const [stats, setStats] = useState<ThumbnailStats | null>(null)
  const [batchStatus, setBatchStatus] = useState<ThumbnailBatchStatus | null>(null)
  const [showDeleteAllConfirm, setShowDeleteAllConfirm] = useState(false)
  const [deletingAll, setDeletingAll] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [showAudit, setShowAudit] = useState(false)
  const pollRef = useRef<number | null>(null)
  const [auditReport, setAuditReport] = useState<ThumbnailAuditReport | null>(null)
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditCleaning, setAuditCleaning] = useState(false)

  const refreshStats = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getThumbnailStats()
      setStats(response.data.data || null)
    } catch {
      setStats(null)
    }
  }, [])

  const refreshBatchStatus = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getThumbnailBatchStatus()
      setBatchStatus(response.data.data || null)
      return response.data.data || null
    } catch {
      return null
    }
  }, [])

  // 完整性检查：打开弹窗时实时拉取（与 HighlightsBatchPanel 模式一致）
  const openAudit = async () => {
    setShowAudit(true)
    setAuditLoading(true)
    try {
      const response = await mediaAnalysisApi.getThumbnailAudit()
      setAuditReport(response.data.data || { total: 0, generated: 0, poster_deleted: [], thumb_missing: [], orphan_thumbs: [] })
    } catch (error: any) {
      toast.error(formatErrMsg(error, '完整性检查失败'))
      setShowAudit(false)
    } finally {
      setAuditLoading(false)
    }
  }

  // 清理失效缩略图
  const handleClean = async () => {
    if (!auditReport) return
    setAuditCleaning(true)
    try {
      const response = await mediaAnalysisApi.cleanThumbnailAuditIssues()
      toast.success(response.data.message || '清理完成')
      setShowAudit(false)
      await refreshStats()
    } catch (error: any) {
      toast.error(formatErrMsg(error, '清理失败'))
    } finally {
      setAuditCleaning(false)
    }
  }

  useEffect(() => {
    void refreshStats()
    void refreshBatchStatus()
  }, [refreshStats, refreshBatchStatus])

  const running = !!batchStatus?.running

  useEffect(() => {
    const delay = running ? 1500 : 5000
    pollRef.current = window.setInterval(() => {
      void refreshStats()
      void refreshBatchStatus()
    }, delay)
    return () => {
      if (pollRef.current) {
        window.clearInterval(pollRef.current)
        pollRef.current = null
      }
    }
  }, [refreshStats, refreshBatchStatus, running])

  const handleStartBatch = async () => {
    try {
      const response = await mediaAnalysisApi.startThumbnailBatch()
      setBatchStatus(response.data.data || null)
      toast.success(response.data.message || '批量生成已启动')
    } catch (error: any) {
      toast.error(formatErrMsg(error, '启动批量生成失败'))
    }
  }

  const handleStopBatch = async () => {
    setStopping(true)
    try {
      const response = await mediaAnalysisApi.stopThumbnailBatch()
      setBatchStatus(response.data.data || null)
      toast.info('已请求停止')
    } catch (error: any) {
      toast.error(formatErrMsg(error, '停止失败'))
    } finally {
      setStopping(false)
    }
  }

  const handleDeleteAll = async () => {
    setDeletingAll(true)
    try {
      const response = await mediaAnalysisApi.batchDeleteThumbnails()
      toast.success(`已删除 ${response.data.data.deleted} 张缩略图`)
      setShowDeleteAllConfirm(false)
      await refreshStats()
    } catch (error: any) {
      toast.error(formatErrMsg(error, '删除失败'))
    } finally {
      setDeletingAll(false)
    }
  }

  const total = stats?.total ?? 0
  const generated = stats?.generated ?? 0
  const missing = stats?.missing ?? 0
  const coveragePercent = total > 0 ? clampPercent((generated / total) * 100) : 0

  // 批量进度
  const bTotal = batchStatus?.total || 0
  const bProcessed = (batchStatus?.generated || 0) + (batchStatus?.skipped || 0) + (batchStatus?.failed || 0)
  const bRemaining = batchStatus?.remaining ?? 0
  const globalPercent = bTotal > 0 ? clampPercent((bProcessed / bTotal) * 100) : 0

  const hasAuditIssues =
    auditReport &&
    ((auditReport.poster_deleted?.length || 0) + (auditReport.thumb_missing?.length || 0) + (auditReport.orphan_thumbs?.length || 0)) > 0

  return (
    <>
      <AdminPanel
        title="海报缩略图管理"
        description="为视频海报自动生成 240px WebP 缩略图，加速移动端首屏加载。"
        icon={<Image size={18} />}
        actions={(
          <>
            {/* 检查完整性：与 HighlightsBatchPanel 模式一致 */}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void openAudit()}
              disabled={running}
              aria-label="检查完整性"
              title={running ? '批量运行期间不可用' : '检查缩略图完整性（原图已删除 / 缩略图缺失 / 孤儿缩略图）'}
            >
              <RefreshCw size={14} className={auditLoading ? 'animate-spin' : undefined} />
              <span className="hidden md:inline">检查完整性</span>
            </Button>
            {running ? (
              <Button variant="danger" size="sm" onClick={() => handleStopBatch()} disabled={stopping}>
                <Square size={14} />
                停止
              </Button>
            ) : (
              <Button
                variant="primary"
                size="sm"
                onClick={() => void handleStartBatch()}
                aria-label="生成缩略图"
              >
                <Sparkles size={15} />
                生成缩略图
              </Button>
            )}
            {!running && (
              <Button
                variant="danger"
                size="sm"
                onClick={() => setShowDeleteAllConfirm(true)}
                disabled={generated === 0}
                aria-label="删除全部缩略图"
              >
                <Trash2 size={14} />
              </Button>
            )}
          </>
        )}
      >
        {running ? (
          <div className="space-y-4 px-1 py-2">
            <div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">生成进度</h3>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                  <span className="whitespace-nowrap">总数 <b className="text-[var(--nv-text-primary)]">{bTotal}</b></span>
                  <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{batchStatus?.generated || 0}</b></span>
                  <span className="whitespace-nowrap">未处理 <b className="text-[var(--nv-text-primary)]">{bRemaining}</b></span>
                  {((batchStatus?.skipped || 0) > 0 || (batchStatus?.failed || 0) > 0) && (
                    <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">
                      跳过 {batchStatus?.skipped || 0}
                      {(batchStatus?.failed || 0) > 0 && <> · 失败 {batchStatus?.failed}</>}
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

            {batchStatus?.current_title && (
              <div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="min-w-0 max-w-full truncate pr-2 text-sm font-semibold text-[var(--nv-text-primary)]" title={batchStatus.current_title}>
                    当前处理：<span className="font-normal text-[var(--nv-text-secondary)]">{batchStatus.current_title}</span>
                  </h3>
                  {batchStatus.stop_requested && (
                    <span className="rounded-full border border-amber-400/40 bg-amber-400/10 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300">停止中…</span>
                  )}
                </div>
              </div>
            )}
          </div>
        ) : stats ? (
          <div className="space-y-3 px-1 py-2">
            <div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">库内覆盖情况</h3>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                  <span className="whitespace-nowrap">
                    视频总数 <b className="text-[var(--nv-text-primary)]">{total}</b>
                  </span>
                  <span className="whitespace-nowrap">
                    已生成 <b className="text-[var(--nv-status-success)]">{generated}</b>
                  </span>
                  <span className="whitespace-nowrap">
                    未生成{' '}
                    {missing > 0 ? (
                      <b className="text-[var(--nv-status-danger)]">{missing}</b>
                    ) : (
                      <b className="text-[var(--nv-text-primary)]">{missing}</b>
                    )}
                  </span>
                </div>
              </div>
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                <div
                  className="h-full rounded-full bg-[var(--nv-accent)] transition-[width] duration-300"
                  style={{ width: `${coveragePercent}%` }}
                />
              </div>
              <p className="mt-1 text-right text-xs tabular-nums text-[var(--nv-text-tertiary)]">
                覆盖率 {coveragePercent.toFixed(1)}%
              </p>
            </div>

            {batchStatus?.finished_at && (
              <p className="text-xs text-[var(--nv-text-tertiary)]">
                上次生成完成于 {new Date(batchStatus.finished_at).toLocaleString('zh-CN')}：新增 {batchStatus.generated} · 跳过 {batchStatus.skipped}
                {(batchStatus.failed || 0) > 0 && <> · 失败 {batchStatus.failed}</>}
              </p>
            )}
          </div>
        ) : (
          <p className="px-1 py-2 text-sm text-[var(--nv-text-tertiary)]">
            加载统计信息中…
          </p>
        )}
      </AdminPanel>

      {/* 完整性检查弹窗（与 HighlightsBatchPanel 模式一致） */}
      {showAudit && (
        <Modal open size="md" ariaLabel="缩略图完整性检查" onClose={() => { if (!auditCleaning) setShowAudit(false) }}>
          <ModalHeader
            title="缩略图完整性检查"
            description="检查全库缩略图覆盖情况：源海报是否仍存在、缩略图文件是否缺失、磁盘上是否有孤儿缩略图。发现的问题可一键清理，点击「生成」会自动补齐缺失项。"
            onClose={() => setShowAudit(false)}
          />
          <ModalBody>
            {auditLoading ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">正在扫描全库缩略图…</p>
            ) : !auditReport ? (
              <p className="text-sm text-[var(--nv-text-tertiary)]">暂无检查结果。</p>
            ) : (
              <div className="space-y-4">
                <p className="text-xs leading-5 text-[var(--nv-text-tertiary)]">
                  库内共 {auditReport.total} 个视频，其中 {auditReport.generated} 个缩略图已生成且源海报正常。
                </p>

                <AuditIssueSection
                  title={`原图已删除（${auditReport.poster_deleted?.length || 0}）`}
                  emptyText="没有原图已删除的问题。"
                  items={auditReport.poster_deleted || []}
                />
                <AuditIssueSection
                  title={`缩略图缺失（${auditReport.thumb_missing?.length || 0}）`}
                  emptyText="没有缩略图缺失的问题。"
                  items={auditReport.thumb_missing || []}
                />
                <AuditIssueSection
                  title={`孤儿缩略图（${auditReport.orphan_thumbs?.length || 0}）`}
                  emptyText="没有孤儿缩略图文件。"
                  items={auditReport.orphan_thumbs || []}
                />
              </div>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" size="sm" onClick={() => setShowAudit(false)} disabled={auditCleaning}>
              关闭
            </Button>
            {hasAuditIssues && (
              <Button
                variant="danger"
                size="sm"
                loading={auditCleaning}
                onClick={() => void handleClean()}
              >
                清理失效缩略图
              </Button>
            )}
          </ModalFooter>
        </Modal>
      )}

      {showDeleteAllConfirm && (
        <ConfirmDialog
          title="清空全部缩略图"
          tone="danger"
          description="将删除全库所有视频的缩略图文件。海报原图不受影响。"
          hint="此操作不可恢复。"
          confirmLabel="全部清空"
          onConfirm={handleDeleteAll}
          onClose={() => setShowDeleteAllConfirm(false)}
          loading={deletingAll}
        />
      )}
    </>
  )
}
