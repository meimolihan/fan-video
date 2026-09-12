import { useCallback, useEffect, useRef, useState } from 'react'
import { CheckCircle2, FilePlus2, FolderSearch, RefreshCw, ShieldCheck, Square, Trash2 } from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button } from '@/components/design-system'
import ConfirmDialog from '@/components/design-system/ConfirmDialog'
import { Modal, ModalBody, ModalFooter, ModalHeader } from '@/components/design-system'
import { useToast } from '@/components/Toast'
import { mediaAnalysisApi, type LocalHighlightGenStatus, type LocalHighlightScanReport, type ManualHighlightReport } from '@/api/mediaAnalysis'
import { formatErrMsg } from '@/utils/error'
import { invalidateMediaListCaches } from '@/utils/invalidateMediaCaches'

function clampPercent(value: number) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

// 目录扫描报告中的问题分区（多视频目录等），纯文本提示原因
function SkipSection({ items }: { items: NonNullable<LocalHighlightScanReport['skipped']> }) {
  if (items.length === 0) return null
  return (
    <div>
      <h4 className="text-sm font-semibold text-[var(--nv-status-danger)]">
        跳过目录（{items.length}）
      </h4>
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

export default function LocalHighlightsPanel() {
  const toast = useToast()
   const [scan, setScan] = useState<LocalHighlightScanReport | null>(null)
   const [status, setStatus] = useState<LocalHighlightGenStatus | null>(null)
   const [starting, setStarting] = useState(false)
   const [stopping, setStopping] = useState(false)
   const [deleting, setDeleting] = useState(false)
   const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
   const [refreshing, setRefreshing] = useState(false)
   const [verifying, setVerifying] = useState(false)
   const [importReport, setImportReport] = useState<ManualHighlightReport | null>(null)
   const [showImportReport, setShowImportReport] = useState(false)
   const prevRunningRef = useRef(false)

   const refreshStatus = useCallback(async () => {
    try {
      const response = await mediaAnalysisApi.getLocalHighlightGenerationStatus()
      setStatus(response.data.data || null)
    } catch {
      setStatus(null)
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

  // 任务结束或首次挂载：刷新扫描报告
  useEffect(() => {
    void refreshStatus()
    void refreshScan()
  }, [refreshStatus, refreshScan])

  const running = !!status?.running

  useEffect(() => {
    if (prevRunningRef.current && !running) {
      void refreshStatus()
      void refreshScan()
    }
    prevRunningRef.current = running
  }, [running, refreshStatus, refreshScan])

  // 运行期间轮询进度
  useEffect(() => {
    if (!running) return
    const id = window.setInterval(() => void refreshStatus(), 1500)
    return () => window.clearInterval(id)
  }, [running, refreshStatus])

  const handleGenerate = async () => {
    const notGenerated = (scan?.eligible || []).filter((d) => !d.generated).map((d) => d.media_id)
    setStarting(true)
    try {
      const response = await mediaAnalysisApi.generateLocalHighlights({ media_ids: notGenerated.length ? notGenerated : undefined })
      setStatus(response.data.data || null)
      toast.success('本地精彩片段生成任务已启动')
    } catch (error) {
      toast.error(formatErrMsg(error, '启动本地精彩片段生成失败'))
    } finally {
      setStarting(false)
    }
  }

  const handleStop = async () => {
    setStopping(true)
    try {
      const response = await mediaAnalysisApi.stopLocalHighlightGeneration()
      setStatus(response.data.data || null)
      toast.info('已请求停止：剩余目录不再处理，已生成的保留结果')
    } catch (error) {
      toast.error(formatErrMsg(error, '停止失败'))
    } finally {
      setStopping(false)
    }
  }

   const handleVerify = async () => {
    setVerifying(true)
    try {
      const response = await mediaAnalysisApi.verifyLocalHighlights()
      const r = response.data.data
      invalidateMediaListCaches()
      await refreshScan()
      toast.success(`校验完成：检查 ${r.total_checked} 个，修复 ${r.repaired} 个，补记 ${r.completed} 个`)
    } catch (error) {
      toast.error(formatErrMsg(error, '校验本地精彩片段失败'))
    } finally {
      setVerifying(false)
    }
  }

   const handleDelete = async () => {
     setDeleting(true)
     try {
       const response = await mediaAnalysisApi.cleanupLocalHighlights()
       toast.success(response.data.message || `已删除 ${response.data.data.files_deleted} 个文件`)
       invalidateMediaListCaches()
       setShowDeleteConfirm(false)
       await refreshScan()
     } catch (error) {
       toast.error(formatErrMsg(error, '删除本地精彩片段失败'))
     } finally {
       setDeleting(false)
     }
   }

   const handleImportManual = async () => {
     try {
       const response = await mediaAnalysisApi.importManualHighlights()
       setImportReport(response.data.data || null)
       toast.success(response.data.message || '手动片段导入完成')
       invalidateMediaListCaches()
       setShowImportReport(true)
       await refreshScan()
     } catch (error) {
       toast.error(formatErrMsg(error, '导入手动片段失败'))
     }
   }

   const handleRefresh = async () => {
     setRefreshing(true)
     try {
       await refreshStatus()
       await handleImportManual()
       await refreshStatus()
     } finally {
       setRefreshing(false)
     }
   }

   const remaining = status?.remaining ?? 0
   const total = status?.total || 0
   const globalPercent = total > 0 ? clampPercent(((total - remaining) / total) * 100) : 0
   const currentPercent =
     status && (status.current_like || 0) > 0 ? clampPercent(((status.current_done || 0) / status.current_like) * 100) : 0

  const eligible = scan?.eligible || []
  const skippedList = scan?.skipped || []

  return (
    <>
      <AdminPanel
        title="本地精彩片段生成"
        description="增量模式：只处理新入库或失败的影片（自动 ffprobe 分析并在目录生成 .highlights 时间线侧车与缩略图、导入为手动片段），已生成的旧影片不再重复探测；目录内多个视频会报错跳过（请将其移入独立目录）。"
        icon={<FolderSearch size={18} />}
        actions={(
          <>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void handleRefresh()}
                  disabled={running}
                  loading={refreshing}
                  aria-label="刷新并导入手动片段"
                  title="刷新目录扫描，并将各视频同目录的 .highlights 手工片段导入为手动精彩片段"
                >
               <RefreshCw size={14} className={refreshing ? 'animate-spin' : undefined} />
               <span className="hidden md:inline">刷新</span>
             </Button>
            {running && (
              <Button variant="danger" size="sm" onClick={() => void handleStop()} loading={stopping}>
                <Square size={14} />
                停止
              </Button>
            )}
             <Button variant="primary" size="sm" onClick={() => void handleGenerate()} disabled={running} loading={starting}>
               <FilePlus2 size={15} />
               <span className="hidden md:inline">本地生成片段</span>
               <span className="md:hidden">生成片段</span>
             </Button>
              {!running && (
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
              )}
              {!running && (
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => setShowDeleteConfirm(true)}
                  disabled={starting}
                  title="删除本地生成的 .highlights 文件并清除对应手动精彩片段记录"
                  aria-label="删除本地生成的精彩片段"
                >
                  <Trash2 size={14} />
                  <span className="hidden md:inline">删除</span>
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
                  <span className="whitespace-nowrap">待处理 <b className="text-[var(--nv-text-primary)]">{remaining}</b></span>
                  <span className="whitespace-nowrap">已生成 <b className="text-[var(--nv-status-success)]">{status?.processed || 0}</b></span>
                  {(status?.already || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">无变动 {status?.already}</span>}
                  {(status?.skipped || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-text-tertiary)]">跳过 {status?.skipped}</span>}
                  {(status?.failed || 0) > 0 && <span className="whitespace-nowrap text-[var(--nv-status-danger)]">失败 {status?.failed}</span>}
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

            {status?.current_video && (
              <div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="min-w-0 max-w-full truncate pr-2 text-sm font-semibold text-[var(--nv-text-primary)]" title={status.current_dir}>
                    当前生成：<span className="font-normal text-[var(--nv-text-secondary)]">{status.current_video}</span>
                  </h3>
                  {status.stop_requested && (
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
        )         : (
          <div className="space-y-3 px-1 py-2">
            {/* 常驻状态摘要（首次挂载即从 status 读取） */}
            <div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-[var(--nv-text-primary)]">生成状态</h3>
                <span className="text-xs text-[var(--nv-text-tertiary)]">
                  {status?.finished_at
                    ? '上次生成完成于 ' + new Date(status.finished_at).toLocaleString('zh-CN')
                    : '尚未运行过本地生成'}
                </span>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-[var(--nv-text-secondary)]">
                <span>成功 <b className="text-[var(--nv-status-success)]">{status?.processed ?? 0}</b></span>
                <span>无变动 <b className="text-[var(--nv-text-tertiary)]">{status?.already ?? 0}</b></span>
                <span>跳过 <b className="text-[var(--nv-text-tertiary)]">{status?.skipped ?? 0}</b></span>
                <span>失败 <b className={(status?.failed ?? 0) > 0 ? 'text-[var(--nv-status-danger)]' : 'text-[var(--nv-text-tertiary)]'}>{status?.failed ?? 0}</b></span>
              </div>
              {!running && status && status.failed > 0 && status.last_error && (
                <p className="mt-1 rounded-[var(--nv-radius-control)] border border-[var(--nv-status-danger)]/40 bg-[var(--nv-status-danger)]/5 px-3 py-2 text-xs leading-5 text-[var(--nv-status-danger)]">
                  上次失败原因：{status.last_error}
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
                  <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--nv-bg-elevated)]">
                    <div
                      className="h-full rounded-full bg-[var(--nv-accent)] transition-[width] duration-300"
                      style={{ width: `${scan.eligible_dirs > 0 ? clampPercent(scan.already_dirs / scan.eligible_dirs * 100) : 0}%` }}
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
                              : ` · 时长未探测（生成时自动分析）`}
                          </p>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                <SkipSection items={skippedList} />

                {(eligible.length === 0 && skippedList.length === 0) ? (
                  <p className="text-sm text-[var(--nv-text-tertiary)]">媒体库中尚未发现视频目录。</p>
                ) : (
                  <p className="mt-2 rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2 text-xs leading-5 text-[var(--nv-text-tertiary)]">
                    增量模式：只列出待生成/失败的影片，「本地生成片段」仅处理这些影片，已生成的旧影片不会再被探测。同目录多个视频会报错跳过（提示并将视频移入独立目录即可）；「校验」可修复「已生成但产物被删」的状态不一致，之后可重新生成。
                  </p>
                )}
              </>
            ) : (
              <p className="text-sm text-[var(--nv-text-tertiary)]">点击「刷新」扫描媒体库目录归属并导入手工片段。</p>
            )}
          </div>
        )}
      </AdminPanel>

       {showDeleteConfirm && (
         <ConfirmDialog
           title="删除本地生成的精彩片段"
           tone="danger"
           description="将删除各单视频目录下由本功能生成的 .highlights 侧车文件（json / 缩略图 / 旧的切割片段），并清除对应的「手动」精彩片段记录。源视频不受影响，之后可重新点「本地生成片段」。"
           hint="仅处理单视频目录下的生成物；手工导入的其它片段不受影响。此操作不可恢复。"
           confirmLabel="删除"
           onConfirm={() => void handleDelete()}
           onClose={() => setShowDeleteConfirm(false)}
           loading={deleting}
         />
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

 function ResultStat({ label, value }: { label: string; value: number }) {
   return (
     <div className="rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] px-3 py-2 text-center">
       <div className="text-lg font-semibold tabular-nums text-[var(--nv-text-primary)]">{value}</div>
       <div className="text-xs text-[var(--nv-text-tertiary)]">{label}</div>
     </div>
   )
 }