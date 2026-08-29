import { userApi } from '@/api'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { Button, EmptyState } from '@/components/design-system'
import { useTranslation } from '@/i18n'
import { usePageCache, invalidatePageCachePrefix } from '@/hooks/usePageCache'
import { usePagination } from '@/hooks/usePagination'
import type { Favorite } from '@/types'
import MediaCard from '@/components/MediaCard'
import VirtualGrid from '@/components/VirtualGrid'
import Pagination from '@/components/Pagination'
import {
  MediaGrid as SharedMediaGrid,
  PersonalWorkspace,
  PersonalWorkspaceHeader,
  PersonalWorkspacePanel,
} from '@/ui'
import { Heart, Trash2 } from 'lucide-react'

interface FavoritesData {
  list: Favorite[]
  total: number
}

export default function FavoritesPage() {
  const { page, size, setPage, setSize, totalPages } = usePagination({
    initialSize: 30,
    syncToUrl: true,
  })
  const toast = useToast()
  const { t } = useTranslation()
  const dialog = useDialog()

  const { data, loading, error, mutate, refetch } = usePageCache<FavoritesData>(
    `favorites:page=${page}:size=${size}`,
    async () => {
      const res = await userApi.favorites(page, size)
      return { list: res.data.data || [], total: res.data.total }
    },
    { ttl: 15_000 },
  )

  if (error) toast.error(t('favorites.loadFailed'))

  const favorites = data?.list ?? []
  const total = data?.total ?? 0
  const media = favorites.map((favorite) => favorite.media).filter(Boolean)
  const pages = totalPages(total)

  const handleClear = async () => {
    const ok = await dialog.confirm({
      title: t('favorites.clearConfirmTitle'),
      message: t('favorites.clearConfirm'),
      confirmText: t('favorites.clear'),
      variant: 'danger',
    })
    if (!ok) return
    try {
      await userApi.clearFavorites()
      mutate({ list: [], total: 0 })
      invalidatePageCachePrefix('favorites:')
      invalidatePageCachePrefix('home:')
      refetch(true)
      toast.success(t('favorites.cleared'))
    } catch {
      toast.error(t('favorites.clearFailed'))
    }
  }

  return (
    <PersonalWorkspace className="nv-favorites-page">
      <PersonalWorkspaceHeader
        icon={<Heart size={20} />}
        title={t('favorites.title')}
        description="把喜欢的电影与剧集留在一个更容易再次找到的位置。"
        statValue={total}
        statLabel="个收藏"
        statAriaLabel={`共 ${total} 个收藏`}
        actions={total > 0 ? (
          <Button variant="danger" size="sm" onClick={handleClear}>
            <Trash2 size={14} aria-hidden="true" />
            {t('favorites.clearAll')}
          </Button>
        ) : undefined}
      />

      <PersonalWorkspacePanel
        titleId="favorite-media-title"
        title="收藏内容"
        description={total > 0 ? `当前共有 ${total} 个收藏，按最近收藏内容浏览。` : '收藏的内容会集中显示在这里。'}
        count={total > 0 ? <span className="text-[var(--nv-status-warning)] font-bold">{total} 项</span> : undefined}
      >
        {loading && (
          <SharedMediaGrid aria-busy="true" aria-label="正在加载收藏内容">
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
          <VirtualGrid count={media.length} minItemWidth={150} aria-label="收藏内容网格">
            {(index) => <MediaCard key={media[index].id} media={media[index]} />}
          </VirtualGrid>
        )}

        {!loading && media.length === 0 && (
          <EmptyState
            className="nv-personal-workspace-empty"
            icon={<Heart size={26} aria-hidden="true" />}
            title={t('favorites.empty')}
            description={t('favorites.emptyHint')}
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
