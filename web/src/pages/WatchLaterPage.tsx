import { userApi } from '@/api'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { Button, EmptyState } from '@/components/design-system'
import { useTranslation } from '@/i18n'
import { usePageCache, invalidatePageCachePrefix } from '@/hooks/usePageCache'
import { usePagination } from '@/hooks/usePagination'
import type { WatchLaterItem } from '@/types'
import MediaCard from '@/components/MediaCard'
import VirtualGrid from '@/components/VirtualGrid'
import Pagination from '@/components/Pagination'
import {
  MediaGrid as SharedMediaGrid,
  PersonalWorkspace,
  PersonalWorkspaceHeader,
  PersonalWorkspacePanel,
} from '@/ui'
import { Bookmark, Trash2 } from 'lucide-react'

interface WatchLaterData {
  list: WatchLaterItem[]
  total: number
}

export default function WatchLaterPage() {
  const { page, size, setPage, setSize, totalPages } = usePagination({
    initialSize: 30,
    syncToUrl: true,
  })
  const toast = useToast()
  const { t } = useTranslation()
  const dialog = useDialog()

  const { data, loading, error, mutate, refetch } = usePageCache<WatchLaterData>(
    `watch-later:page=${page}:size=${size}`,
    async () => {
      const res = await userApi.watchLater(page, size)
      return { list: res.data.data || [], total: res.data.total }
    },
    { ttl: 15_000 },
  )

  if (error) toast.error(t('watchLater.loadFailed'))

  const items = data?.list ?? []
  const total = data?.total ?? 0
  const media = items.map((item) => item.media).filter(Boolean)
  const pages = totalPages(total)

  const handleClear = async () => {
    const ok = await dialog.confirm({
      title: t('watchLater.clearConfirmTitle'),
      message: t('watchLater.clearConfirm'),
      confirmText: t('watchLater.clear'),
      variant: 'danger',
    })
    if (!ok) return
    try {
      await userApi.clearWatchLater()
      mutate({ list: [], total: 0 })
      invalidatePageCachePrefix('watch-later:')
      refetch(true)
      toast.success(t('watchLater.cleared'))
    } catch {
      toast.error(t('watchLater.clearFailed'))
    }
  }

  return (
    <PersonalWorkspace className="nv-watch-later-page">
      <PersonalWorkspaceHeader
        icon={<Bookmark size={20} />}
        title={t('watchLater.title')}
        description="把想看的电影与剧集先收起来，稍后再看。"
        statValue={total}
        statLabel="个条目"
        statAriaLabel={`共 ${total} 个条目`}
        actions={total > 0 ? (
          <Button variant="danger" size="sm" onClick={handleClear}>
            <Trash2 size={14} aria-hidden="true" />
            {t('watchLater.clearAll')}
          </Button>
        ) : undefined}
      />

      <PersonalWorkspacePanel
        titleId="watch-later-media-title"
        title="稍后再看内容"
        description={total > 0 ? <>当前共有 <span className="text-[var(--nv-status-warning)] font-bold">{total}</span> 个条目，按添加时间浏览。</> : '标记为稍后再看的内容会集中显示在这里。'}
        count={total > 0 ? <span className="font-bold"><span className="text-[var(--nv-status-warning)]">{total}</span> 项</span> : undefined}
      >
        {loading && (
          <SharedMediaGrid aria-busy="true" aria-label="正在加载稍后再看内容">
            {Array.from({ length: 10 }).map((_, index) => (
              <div key={index}>
                <div className="skeleton aspect-[2/3] rounded-[var(--nv-radius-card)]" />
                <div className="skeleton mt-2 h-3 w-3/4" />
                <div className="skeleton mt-1.5 h-2.5 w-1/2" />
              </div>
            ))}
          </SharedMediaGrid>
        )}

        {!loading && media.length > 0 && (
          <VirtualGrid count={media.length} minItemWidth={150} aria-label="稍后再看内容网格">
            {(index) => <MediaCard key={media[index].id} media={media[index]} />}
          </VirtualGrid>
        )}

        {!loading && media.length === 0 && (
          <EmptyState
            className="nv-personal-workspace-empty"
            icon={<Bookmark size={26} aria-hidden="true" />}
            title={t('watchLater.empty')}
            description={t('watchLater.emptyHint')}
          />
        )}

        {total > 0 && (
          <div className="nv-personal-workspace-pagination">
            <Pagination
              page={page}
              totalPages={pages}
              total={total}
              pageSize={size}
              pageSizeOptions={[20, 30, 50, 100]}
              onPageChange={setPage}
              onPageSizeChange={setSize}
            />
          </div>
        )}
      </PersonalWorkspacePanel>
    </PersonalWorkspace>
  )
}
