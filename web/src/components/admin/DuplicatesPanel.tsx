import { useCallback, useEffect, useState } from 'react'
import { Copy, Layers, RefreshCw } from 'lucide-react'
import { adminApi } from '@/api'
import { useToast } from '@/components/Toast'
import { Button, Tag } from '@/components/design-system'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { bumpPosterVersion } from '@/stores/mediaRefresh'
import type { DuplicateGroup, Library } from '@/types'

function formatSize(bytes: number) {
  if (!bytes) return '未知大小'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1) return `${gb.toFixed(1)}GB`
  return `${(bytes / (1024 * 1024)).toFixed(0)}MB`
}

const typeLabels: Record<string, string> = {
  movie: '电影',
  episode: '剧集',
}

export default function DuplicatesPanel({ libraries }: { libraries: Library[] }) {
  const toast = useToast()
  const [groups, setGroups] = useState<DuplicateGroup[]>([])
  const [loading, setLoading] = useState(false)
  const [folding, setFolding] = useState(false)

  const detect = useCallback(async () => {
    setLoading(true)
    try {
      const response = await adminApi.detectDuplicates()
      setGroups(response.data.data || [])
    } catch {
      toast.error('重复媒体检测失败')
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => {
    void detect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const foldAll = async () => {
    if (libraries.length === 0) return
    setFolding(true)
    try {
      let marked = 0
      for (const library of libraries) {
        const response = await adminApi.markDuplicates(library.id)
        marked += response.data.marked || 0
      }
      bumpPosterVersion()
      await detect()
      toast.success(marked > 0 ? `已折叠 ${marked} 个重复副本，列表将只显示主版本` : '未发现可折叠的重复副本')
    } catch {
      toast.error('标记重复媒体失败')
    } finally {
      setFolding(false)
    }
  }

  const copyPaths = (group: DuplicateGroup) => {
    navigator.clipboard.writeText(group.media.map((item) => item.file_path).join('\n'))
      .then(() => toast.success('路径已复制'))
      .catch(() => {})
  }

  return (
    <AdminPanel
      title="重复媒体"
      icon={<Layers size={15} className="text-[var(--nv-action-primary)]" />}
      description="同一部影片出现多张相同海报时，系统会自动折叠为一张卡片，仅保留画质最好的主版本；其余副本仍可在详情页切换版本播放。"
      actions={(
        <>
          <Button variant="secondary" size="sm" onClick={() => void detect()} disabled={loading}>
            <RefreshCw size={14} className={loading ? 'animate-spin' : undefined} />
            重新检测
          </Button>
          <Button variant="primary" size="sm" onClick={() => void foldAll()} disabled={folding || loading || groups.length === 0}>
            {folding ? '折叠中…' : '标记并折叠所有副本'}
          </Button>
        </>
      )}
    >
      {groups.length === 0 && !loading && (
        <p className="text-sm text-[var(--nv-text-tertiary)]">未检测到重复的媒体记录。</p>
      )}

      {groups.length > 0 && (
        <ul className="space-y-3">
          {groups.map((group) => (
            <li key={group.group_key} className="rounded-[var(--nv-radius-card)] border border-[var(--nv-border-subtle)] p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-semibold text-[var(--nv-text-primary)]">{group.title}</span>
                {group.year > 0 && <span className="text-xs text-[var(--nv-text-tertiary)]">{group.year}</span>}
                <Tag tone={group.media_count > 2 ? 'warning' : 'neutral'}>{group.media_count} 个版本</Tag>
                <button
                  type="button"
                  onClick={() => copyPaths(group)}
                  className="ml-auto inline-flex items-center gap-1 text-xs text-[var(--nv-text-tertiary)] transition-colors hover:text-[var(--nv-text-primary)]"
                  aria-label="复制文件路径"
                >
                  <Copy size={12} />
                  复制路径
                </button>
              </div>

              <ul className="mt-2 space-y-1.5">
                {group.media.map((item) => (
                  <li key={item.id} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-[var(--nv-text-secondary)]">
                    {item.is_primary ? (
                      <Tag tone="success">保留</Tag>
                    ) : (
                      <Tag tone="neutral">折叠</Tag>
                    )}
                    <span className="truncate" title={item.file_path}>{item.file_path}</span>
                    <span className="shrink-0 text-[var(--nv-text-tertiary)]">
                      {[
                        typeLabels[item.media_type] || item.media_type,
                        item.resolution,
                        formatSize(item.file_size),
                      ].filter(Boolean).join(' · ')}
                    </span>
                  </li>
                ))}
              </ul>

              {group.suggestion && (
                <p className="mt-2 text-xs text-[var(--nv-text-tertiary)]">{group.suggestion}</p>
              )}
            </li>
          ))}
        </ul>
      )}
    </AdminPanel>
  )
}
