import { useMemo } from 'react'
import { Calendar, Clock, Film, Grid3X3, LayoutList, Play, Star } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { streamApi } from '@/api'
import type { CollectionMediaItem } from '@/types'
import { Button, Select, Tag } from '@/components/design-system'
import { MediaArtwork, MediaGrid } from '@/ui'
import Pagination from '@/components/Pagination'
import { usePagination } from '@/hooks/usePagination'

type SortOption = 'premiered_asc' | 'premiered_desc' | 'title_asc' | 'rating_desc'

const SORT_OPTIONS: { value: SortOption; label: string }[] = [
  { value: 'premiered_asc', label: '首映日期 ↑' },
  { value: 'premiered_desc', label: '首映日期 ↓' },
  { value: 'title_asc', label: '标题 A-Z' },
  { value: 'rating_desc', label: '评分 ↓' },
]

interface CollectionMovieBrowserProps {
  media: CollectionMediaItem[]
}

export default function CollectionMovieBrowser({ media }: CollectionMovieBrowserProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const viewMode = (searchParams.get('view') || 'grid') as 'grid' | 'list'
  const sortOption = (searchParams.get('sort') || 'premiered_asc') as SortOption
  const pagination = usePagination({ initialSize: 24, syncToUrl: true })

  const sortedMedia = useMemo(() => {
    const sorted = [...media]
    const byPremiered = (direction: 'asc' | 'desc') => (a: CollectionMediaItem, b: CollectionMediaItem) => {
      const dateA = a.premiered || ''
      const dateB = b.premiered || ''
      if (dateA && dateB) {
        const compared = direction === 'asc' ? dateA.localeCompare(dateB) : dateB.localeCompare(dateA)
        return compared || a.title.localeCompare(b.title)
      }
      if (dateA) return -1
      if (dateB) return 1
      const yearA = a.year || (direction === 'asc' ? 9999 : 0)
      const yearB = b.year || (direction === 'asc' ? 9999 : 0)
      const yearCompared = direction === 'asc' ? yearA - yearB : yearB - yearA
      return yearCompared || a.title.localeCompare(b.title)
    }

    if (sortOption === 'premiered_asc') sorted.sort(byPremiered('asc'))
    if (sortOption === 'premiered_desc') sorted.sort(byPremiered('desc'))
    if (sortOption === 'title_asc') sorted.sort((a, b) => a.title.localeCompare(b.title))
    if (sortOption === 'rating_desc') sorted.sort((a, b) => b.rating - a.rating || a.title.localeCompare(b.title))
    return sorted
  }, [media, sortOption])

  const pagedMedia = useMemo(() => {
    const start = (pagination.page - 1) * pagination.size
    return sortedMedia.slice(start, start + pagination.size)
  }, [pagination.page, pagination.size, sortedMedia])

  const setSort = (value: SortOption) => {
    const params = new URLSearchParams(searchParams)
    if (value === 'premiered_asc') params.delete('sort')
    else params.set('sort', value)
    params.delete('page')
    setSearchParams(params, { replace: true })
  }

  const setView = (view: 'grid' | 'list') => {
    const params = new URLSearchParams(searchParams)
    if (view === 'grid') params.delete('view')
    else params.set('view', view)
    setSearchParams(params, { replace: true })
  }

  return (
    <section className="space-y-5">
      <div className="flex flex-col gap-3 border-b border-[var(--nv-border-subtle)] pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="font-display text-lg font-semibold tracking-tight text-[var(--nv-text-primary)]">系列视频</h2>
          <p className="mt-1 text-xs text-[var(--nv-text-tertiary)]"><span className="text-[var(--nv-status-warning)] font-bold">{sortedMedia.length}</span> 部视频</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Select value={sortOption} onChange={(event) => setSort(event.target.value as SortOption)} className="h-9 text-xs" aria-label="合集电影排序">
            {SORT_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </Select>
          <div className="flex items-center gap-0.5 rounded-[var(--nv-radius-control)] border border-[var(--nv-border-default)] p-0.5" role="group" aria-label="视图模式">
            <Button type="button" variant="ghost" size="sm" iconOnly onClick={() => setView('grid')} aria-label="卡片视图" title="卡片视图" aria-pressed={viewMode === 'grid'} className={viewMode === 'grid' ? '!bg-[var(--nv-fill-active)] !text-[var(--nv-text-primary)]' : undefined}><Grid3X3 size={14} /></Button>
            <Button type="button" variant="ghost" size="sm" iconOnly onClick={() => setView('list')} aria-label="列表视图" title="列表视图" aria-pressed={viewMode === 'list'} className={viewMode === 'list' ? '!bg-[var(--nv-fill-active)] !text-[var(--nv-text-primary)]' : undefined}><LayoutList size={14} /></Button>
          </div>
        </div>
      </div>

      {viewMode === 'grid' ? (
        <MediaGrid>
          {pagedMedia.map((item, index) => (
            <MovieGridCard key={item.id} item={item} index={(pagination.page - 1) * pagination.size + index + 1} />
          ))}
        </MediaGrid>
      ) : (
        <div className="divide-y divide-[var(--nv-border-subtle)] border-y border-[var(--nv-border-subtle)]">
          {pagedMedia.map((item, index) => (
            <MovieListCard key={item.id} item={item} index={(pagination.page - 1) * pagination.size + index + 1} />
          ))}
        </div>
      )}

      <Pagination
        page={pagination.page}
        totalPages={pagination.totalPages(sortedMedia.length)}
        total={sortedMedia.length}
        pageSize={pagination.size}
        pageSizeOptions={[12, 24, 36, 48]}
        onPageChange={pagination.setPage}
        onPageSizeChange={pagination.setSize}
      />
    </section>
  )
}

function MovieGridCard({ item, index }: { item: CollectionMediaItem; index: number }) {
  const navigate = useNavigate()

  return (
    <article className="nv-media-card group relative">
      <MediaArtwork
        src={item.poster_path ? streamApi.getPosterUrl(item.id) : null}
        alt={item.title}
        ratio="poster"
        className="nv-media-card-poster"
        imageClassName="nv-media-card-image"
        fallback={(
          <div className="flex flex-col items-center justify-center gap-2 text-[var(--nv-text-tertiary)]">
            <Film size={24} aria-hidden="true" />
            <span className="text-[10px]">暂无海报</span>
          </div>
        )}
      >
        <Link to={`/media/${item.id}`} className="absolute inset-0 z-10 rounded-[inherit]" aria-label={`查看 ${item.title} 详情`} />

        <div className="nv-media-card-overlay z-20 pointer-events-none">
          <Button
            variant="primary"
            size="sm"
            iconOnly
            className="nv-media-card-play pointer-events-auto"
            onClick={() => navigate(`/play/${item.id}`)}
            aria-label={`播放 ${item.title}`}
            title="立即播放"
          >
            <Play size={16} fill="currentColor" aria-hidden="true" />
          </Button>
        </div>

        <Tag className="nv-media-card-badge absolute left-2 top-2 z-30">#{index}</Tag>
        {item.rating > 0 && (
          <Tag tone="rating" className="nv-media-card-badge absolute bottom-2 left-2 z-30"><Star size={10} fill="currentColor" aria-hidden="true" />{item.rating.toFixed(1)}</Tag>
        )}
      </MediaArtwork>

      <div className="pb-1 pt-2">
        <Link to={`/media/${item.id}`} className="nv-media-card-title" title={item.title}>{item.title}</Link>
        <div className="nv-media-card-meta mt-1 flex min-w-0 items-center gap-1.5 overflow-hidden">
          {item.year > 0 && <span className="inline-flex shrink-0 items-center gap-1"><Calendar size={10} aria-hidden="true" />{item.year}</span>}
          {item.runtime > 0 && (
            <>
              {item.year > 0 && <span aria-hidden="true">·</span>}
              <span className="inline-flex shrink-0 items-center gap-1"><Clock size={10} aria-hidden="true" />{item.runtime} 分钟</span>
            </>
          )}
        </div>
      </div>
    </article>
  )
}

function MovieListCard({ item, index }: { item: CollectionMediaItem; index: number }) {
  const navigate = useNavigate()

  return (
    <article className="nv-browse-list-item group transition-colors hover:bg-[var(--nv-fill-hover)]">
      <div className="flex items-center gap-3 px-1 py-2.5">
        <span className="w-6 shrink-0 text-center text-[10px] font-semibold tabular-nums text-[var(--nv-text-tertiary)]">{index}</span>
        <Link to={`/media/${item.id}`} className="block h-16 w-11 shrink-0">
          <MediaArtwork
            src={item.poster_path ? streamApi.getPosterUrl(item.id) : null}
            alt=""
            ratio="poster"
            className="h-16 w-11 !rounded-[9px]"
            fallback={<Film size={15} aria-hidden="true" />}
          />
        </Link>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Link to={`/media/${item.id}`} className="min-w-0 truncate text-xs font-medium text-[var(--nv-text-primary)]">{item.title}</Link>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 text-[10px] text-[var(--nv-text-tertiary)]">
            {item.year > 0 && <span>{item.year}</span>}
            {item.runtime > 0 && <span>{formatDuration(item.runtime)}</span>}
          </div>
        </div>

        {item.rating > 0 && <Tag tone="rating" className="shrink-0"><Star size={10} fill="currentColor" aria-hidden="true" />{item.rating.toFixed(1)}</Tag>}
        <Button
          variant="primary"
          size="sm"
          iconOnly
          className="nv-collection-list-play shrink-0"
          onClick={() => navigate(`/play/${item.id}`)}
          aria-label={`播放 ${item.title}`}
          title="立即播放"
        >
          <Play size={13} fill="currentColor" aria-hidden="true" />
        </Button>
      </div>
    </article>
  )
}

function formatDuration(seconds: number) {
  if (!seconds) return ''
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`
}
