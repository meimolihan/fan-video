import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Clock, GalleryHorizontal, LayoutList, Play } from 'lucide-react'
import type { Media, SeasonInfo, WatchHistory } from '@/types'
import { streamApi } from '@/api'
import { Button, EmptyState, Tag } from '@/components/design-system'
import Pagination from '@/components/Pagination'
import { usePagination } from '@/hooks/usePagination'
import { MediaArtwork } from '@/ui'

interface SeriesEpisodeBrowserProps {
  seasons: SeasonInfo[]
  seriesTitle: string
  historyMap: Record<string, WatchHistory>
  posterVersion: number
  preferredSeason?: number
}

type DisplayMode = 'slide' | 'list'

const EPISODE_PAGE_THRESHOLD = 50

export default function SeriesEpisodeBrowser({ seasons, seriesTitle, historyMap, posterVersion }: SeriesEpisodeBrowserProps) {
  const [displayMode, setDisplayMode] = useState<DisplayMode>('slide')
  const pagination = usePagination({ initialSize: 50 })

  // 平铺展示系列下的所有视频：不再按季分组，统一按顺序编排。
  const allEpisodes = useMemo(() => seasons
    .flatMap((season) => season.episodes || [])
    .slice()
    .sort((left, right) => left.season_num - right.season_num || left.episode_num - right.episode_num || (left.id > right.id ? 1 : -1)), [seasons])

  const needsPagination = allEpisodes.length > EPISODE_PAGE_THRESHOLD
  const totalPages = Math.max(1, Math.ceil(allEpisodes.length / pagination.size))

  useEffect(() => {
    if (pagination.page > totalPages) pagination.setPage(totalPages)
  }, [pagination.page, pagination.setPage, totalPages])

  const pagedEpisodes = useMemo(() => {
    if (!needsPagination) return allEpisodes
    const start = (pagination.page - 1) * pagination.size
    return allEpisodes.slice(start, start + pagination.size)
  }, [allEpisodes, needsPagination, pagination.page, pagination.size])

  const watchedCount = useMemo(() => allEpisodes.filter((episode) => getWatchStatus(historyMap[episode.id]).watched).length, [allEpisodes, historyMap])
  const inProgressCount = useMemo(() => allEpisodes.filter((episode) => {
    const status = getWatchStatus(historyMap[episode.id])
    return !status.watched && status.progress > 0
  }).length, [allEpisodes, historyMap])

  if (allEpisodes.length === 0) {
    return <EmptyState title="暂无内容" description="当前系列还没有可展示的视频。" className="min-h-52" />
  }

  return (
    <section className="nv-series-episode-browser space-y-5">
      <div className="nv-series-episode-toolbar flex flex-col gap-3 border-b border-[var(--nv-border-subtle)] pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <Tag>共 <span className="text-[var(--nv-status-warning)] font-bold">{allEpisodes.length}</span> 个视频</Tag>
          {watchedCount > 0 && <Tag tone="success">已看 {watchedCount}/{allEpisodes.length}</Tag>}
          {inProgressCount > 0 && <Tag tone="brand">进行中 {inProgressCount}</Tag>}
        </div>

        <div className="flex items-center gap-1 rounded-[var(--nv-radius-control)] border border-[var(--nv-border-subtle)] bg-[var(--nv-bg-surface-soft)] p-1">
          <Button type="button" variant={displayMode === 'slide' ? 'secondary' : 'ghost'} size="sm" iconOnly onClick={() => setDisplayMode('slide')} aria-label="幻灯片模式" title="幻灯片模式">
            <GalleryHorizontal size={15} aria-hidden="true" />
          </Button>
          <Button type="button" variant={displayMode === 'list' ? 'secondary' : 'ghost'} size="sm" iconOnly onClick={() => setDisplayMode('list')} aria-label="列表模式" title="列表模式">
            <LayoutList size={15} aria-hidden="true" />
          </Button>
        </div>
      </div>

      {displayMode === 'slide' ? (
        <EpisodeSlider
          episodes={pagedEpisodes}
          seriesTitle={seriesTitle}
          historyMap={historyMap}
          posterVersion={posterVersion}
        />
      ) : (
        <div className="space-y-2">
          {pagedEpisodes.map((episode) => (
            <EpisodeListCard key={episode.id} episode={episode} seriesTitle={seriesTitle} historyRecord={historyMap[episode.id]} posterVersion={posterVersion} />
          ))}
        </div>
      )}

      {needsPagination && (
        <Pagination
          page={pagination.page}
          totalPages={totalPages}
          total={allEpisodes.length}
          pageSize={pagination.size}
          pageSizeOptions={[20, 50, 100, 200]}
          onPageChange={pagination.setPage}
          onPageSizeChange={pagination.setSize}
        />
      )}
    </section>
  )
}

function EpisodeSlider({
  episodes,
  seriesTitle,
  historyMap,
  posterVersion,
}: {
  episodes: Media[]
  seriesTitle: string
  historyMap: Record<string, WatchHistory>
  posterVersion: number
}) {
  const sliderRef = useRef<HTMLDivElement>(null)
  const scrollBy = (left: number) => sliderRef.current?.scrollBy({ left, behavior: 'smooth' })

  if (episodes.length === 0) {
    return <EmptyState title="暂无内容" description="当前没有可展示的视频。" className="min-h-44" />
  }

  return (
    <div className="group/series-slider relative">
      <Button type="button" variant="secondary" size="sm" iconOnly onClick={() => scrollBy(-360)} className="absolute -left-2 top-1/2 z-10 -translate-y-1/2 opacity-0 shadow-[var(--nv-shadow-card)] transition-opacity group-hover/series-slider:opacity-100" aria-label="向左滚动">
        <ChevronLeft size={16} aria-hidden="true" />
      </Button>

      <div ref={sliderRef} className="flex snap-x snap-mandatory gap-3 overflow-x-auto pb-2 scrollbar-hide" style={{ scrollbarWidth: 'none' }}>
        {episodes.map((episode) => (
          <EpisodeSlideCard key={episode.id} episode={episode} seriesTitle={seriesTitle} historyRecord={historyMap[episode.id]} posterVersion={posterVersion} />
        ))}
      </div>

      <Button type="button" variant="secondary" size="sm" iconOnly onClick={() => scrollBy(360)} className="absolute -right-2 top-1/2 z-10 -translate-y-1/2 opacity-0 shadow-[var(--nv-shadow-card)] transition-opacity group-hover/series-slider:opacity-100" aria-label="向右滚动">
        <ChevronRight size={16} aria-hidden="true" />
      </Button>
    </div>
  )
}

function EpisodeListCard({
  episode,
  seriesTitle,
  historyRecord,
  posterVersion,
}: {
  episode: Media
  seriesTitle: string
  historyRecord?: WatchHistory
  posterVersion: number
}) {
  const status = getWatchStatus(historyRecord)

  return (
    <Link
      to={`/media/${episode.id}`}
      className="nv-episode-list-card group flex items-center gap-3 rounded-[var(--nv-radius-card)] border border-[var(--nv-border-default)] bg-[var(--nv-bg-surface)] p-3 transition-[background-color,border-color,box-shadow,transform] hover:-translate-y-px hover:border-[var(--nv-border-hover)] hover:bg-[var(--nv-bg-hover)] hover:shadow-[var(--nv-shadow-card-hover)]"
    >
      <EpisodeThumb episode={episode} status={status} posterVersion={posterVersion} className="h-16 w-28" />

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className={`min-w-0 flex-1 truncate text-sm font-medium ${status.watched ? 'text-[var(--nv-text-tertiary)]' : 'text-[var(--nv-text-primary)]'}`}>
            {episodeTitle(episode, seriesTitle)}
          </h3>
          {status.watched && <Tag tone="success">已看</Tag>}
        </div>

        <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-[var(--nv-text-tertiary)]">
          {episode.duration > 0 && <span className="inline-flex items-center gap-1"><Clock size={12} />{formatDuration(episode.duration)}</span>}
          {!status.watched && status.progress > 0 && <span className="text-[var(--nv-action-primary)]">{status.progress}%</span>}
          {episode.resolution && <Tag tone="quality">{episode.resolution}</Tag>}
          {episode.video_codec && <Tag>{episode.video_codec}</Tag>}
        </div>

        {episode.overview && <p className="mt-1.5 line-clamp-2 text-xs leading-5 text-[var(--nv-text-tertiary)]">{episode.overview}</p>}
      </div>

      <ChevronRight size={16} className="shrink-0 text-[var(--nv-text-tertiary)] transition-colors group-hover:text-[var(--nv-action-primary)]" aria-hidden="true" />
    </Link>
  )
}

function EpisodeSlideCard({
  episode,
  seriesTitle,
  historyRecord,
  posterVersion,
}: {
  episode: Media
  seriesTitle: string
  historyRecord?: WatchHistory
  posterVersion: number
}) {
  const status = getWatchStatus(historyRecord)

  return (
    <Link
      to={`/media/${episode.id}`}
      className="nv-episode-slide-card group w-[13.5rem] shrink-0 snap-start overflow-hidden rounded-[var(--nv-radius-card)] border border-[var(--nv-border-default)] bg-[var(--nv-bg-surface)] transition-[background-color,border-color,box-shadow,transform] hover:-translate-y-0.5 hover:border-[var(--nv-border-hover)] hover:shadow-[var(--nv-shadow-card-hover)]"
    >
      <EpisodeThumb episode={episode} status={status} posterVersion={posterVersion} className="aspect-video w-full !rounded-none !border-0" />

      <div className="p-3">
        <h3 className={`truncate text-sm font-medium ${status.watched ? 'text-[var(--nv-text-tertiary)]' : 'text-[var(--nv-text-primary)]'}`}>
          {episodeTitle(episode, seriesTitle)}
        </h3>
        <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-[var(--nv-text-tertiary)]">
          {status.watched ? <Tag tone="success">已看</Tag> : status.progress > 0 ? <span className="text-[var(--nv-action-primary)]">{status.progress}%</span> : null}
          {episode.resolution && <Tag tone="quality">{episode.resolution}</Tag>}
          {episode.duration > 0 && <span>{formatDuration(episode.duration)}</span>}
        </div>
        {episode.overview && <p className="mt-2 line-clamp-2 text-[11px] leading-5 text-[var(--nv-text-tertiary)]">{episode.overview}</p>}
      </div>
    </Link>
  )
}

function EpisodeThumb({
  episode,
  status,
  posterVersion,
  className,
}: {
  episode: Media
  status: { watched: boolean; progress: number }
  posterVersion: number
  className: string
}) {
  return (
    <MediaArtwork
      // 不以 poster_path 为前提：后端会在请求时懒提取首帧并回写，
      // 这样没有本地封面的分集也能获得海报
      src={streamApi.getPosterUrl(episode.id, posterVersion)}
      alt={episode.title}
      ratio="landscape"
      className={`nv-episode-thumb shrink-0 ${className}`}
      imageClassName="transition-transform duration-300 group-hover:scale-[1.025]"
      fallback={<Play size={22} aria-hidden="true" />}
    >
      <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/35 opacity-0 transition-opacity group-hover:opacity-100">
        <div className="flex h-9 w-9 items-center justify-center rounded-full bg-[var(--nv-action-primary)] text-[var(--nv-text-on-brand)] shadow-[var(--nv-shadow-card)]">
          <Play size={16} fill="currentColor" className="ml-0.5" aria-hidden="true" />
        </div>
      </div>

      {status.watched && (
        <div className="absolute inset-0 z-30 flex items-center justify-center bg-black/45">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--nv-status-success)] text-white"><Check size={16} aria-hidden="true" /></div>
        </div>
      )}

      {!status.watched && status.progress > 0 && (
        <div className="nv-episode-progress absolute inset-x-0 bottom-0 z-30 h-1 bg-black/35">
          <div className="h-full bg-[var(--nv-action-primary)]" style={{ width: `${status.progress}%` }} />
        </div>
      )}
    </MediaArtwork>
  )
}

function getWatchStatus(historyRecord?: WatchHistory) {
  if (!historyRecord) return { watched: false, progress: 0 }
  const ratio = historyRecord.duration > 0 ? historyRecord.position / historyRecord.duration : 0
  return {
    watched: historyRecord.completed || ratio >= 0.9,
    progress: Math.max(0, Math.min(100, Math.round(ratio * 100))),
  }
}

function episodeTitle(episode: Media, seriesTitle: string) {
  return episode.episode_title || episode.title || seriesTitle
}

function formatDuration(seconds: number) {
  if (!seconds) return ''
  return `${Math.floor(seconds / 60)}分钟`
}
