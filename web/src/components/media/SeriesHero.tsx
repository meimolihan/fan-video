import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Heart, Image, MoreHorizontal, Play, Share2, Star, Trash2, Tv } from 'lucide-react'
import type { Media, Series } from '@/types'
import { streamApi } from '@/api'
import { Button, Tag, buttonClassName } from '@/components/design-system'
import { HeroContent, MediaArtwork } from '@/ui'
import PosterImage from '@/components/PosterImage'

interface SeriesHeroProps {
  series: Series
  episodes: Media[]
  playEpisode: Media | null
  playLabel: string
  isFavorited: boolean
  isAdmin: boolean
  posterVersion: number
  onFavorite: () => void
  onDelete: () => void
  onShare: () => void
  onPosterPicker: () => void
}

const SERIES_POSTER_SWITCH_MS = 6000

export default function SeriesHero({
  series,
  episodes,
  playEpisode,
  playLabel,
  isFavorited,
  isAdmin,
  posterVersion,
  onFavorite,
  onDelete,
  onShare,
  onPosterPicker,
}: SeriesHeroProps) {
  const [imageLoaded, setImageLoaded] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const menuContainerRef = useRef<HTMLDivElement>(null)
  const genres = (series.genres || '').split(',').map((item) => item.trim()).filter(Boolean)

  const episodePosters = useMemo(() => {
    const seen = new Set<string>()
    const urls: string[] = []
    for (const episode of episodes) {
      const url = streamApi.getPosterUrl(episode.id, posterVersion)
      if (!url || seen.has(url)) continue
      seen.add(url)
      urls.push(url)
    }
    return urls
  }, [episodes, posterVersion])

  const [posterShown, setPosterShown] = useState(0)
  const [posterSwitching, setPosterSwitching] = useState(-1)
  const [nextReady, setNextReady] = useState(false)
  const activeTimerRef = useRef<number | null>(null)
  const switchIndexRef = useRef(0)

  useEffect(() => {
    setImageLoaded(false)
  }, [posterVersion, series.backdrop_path, series.id])

  useEffect(() => {
    setPosterShown(0)
    setPosterSwitching(-1)
    setNextReady(false)
    switchIndexRef.current = 0
  }, [series.id])

  // Warm the browser cache up front so a switch never waits on the network.
  useEffect(() => {
    const images: HTMLImageElement[] = []
    for (const url of episodePosters) {
      const img = new window.Image()
      img.src = url
      images.push(img)
    }
    return () => {
      for (const img of images) img.src = ''
    }
  }, [episodePosters])

  useEffect(() => {
    if (episodePosters.length < 2) return
    activeTimerRef.current = window.setInterval(() => {
      const count = episodePosters.length
      let next = switchIndexRef.current
      while (episodePosters.length > 1 && next === switchIndexRef.current) {
        next = Math.floor(Math.random() * count)
      }
      switchIndexRef.current = next
      setPosterSwitching(next)
      setNextReady(false)
    }, SERIES_POSTER_SWITCH_MS)
    return () => {
      if (activeTimerRef.current) window.clearInterval(activeTimerRef.current)
      activeTimerRef.current = null
    }
  }, [episodePosters.length])

  // The incoming poster only fades in once its pixels are decoded, so the
  // crossfade is smooth instead of popping on a half-loaded frame.
  const handlePosterSwitchDone = useCallback(() => {
    if (posterSwitching >= 0 && posterSwitching !== posterShown) {
      setPosterShown(posterSwitching)
      setPosterSwitching(-1)
      setNextReady(false)
    }
  }, [posterShown, posterSwitching])

  const isSwitching = posterSwitching >= 0 && posterSwitching !== posterShown
  const hasEpisodePosters = episodePosters.length > 0
  const showEpisodeSlideshow = hasEpisodePosters

  useEffect(() => {
    if (!menuOpen) return
    const handlePointerDown = (e: PointerEvent) => {
      if (menuContainerRef.current && !menuContainerRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    return () => document.removeEventListener('pointerdown', handlePointerDown, true)
  }, [menuOpen])

  const closeAndRun = (action: () => void) => {
    setMenuOpen(false)
    action()
  }

  const actions = (
    <>
      {playEpisode && (
        <Link to={`/play/${playEpisode.id}`} className={buttonClassName({ variant: 'primary', size: 'lg' })} data-variant="primary" data-size="lg">
          <Play size={17} fill="currentColor" aria-hidden="true" />
          {playLabel}
        </Link>
      )}

      <Button type="button" variant="secondary" size="lg" iconOnly onClick={onFavorite} disabled={!playEpisode} title={isFavorited ? '取消收藏' : '收藏'} aria-label={isFavorited ? '取消收藏' : '收藏'} aria-pressed={isFavorited}>
        <Heart size={18} fill={isFavorited ? 'currentColor' : 'none'} aria-hidden="true" />
      </Button>

      <div className="relative" ref={menuContainerRef}>
        <Button type="button" variant="secondary" size="lg" iconOnly onClick={() => setMenuOpen((open) => !open)} aria-label="更多操作" aria-expanded={menuOpen}>
          <MoreHorizontal size={19} aria-hidden="true" />
        </Button>

        {menuOpen && (
          <div className="nv-menu absolute left-0 top-full z-[var(--nv-z-dropdown)] mt-2 w-56" role="menu">
            {isAdmin && (
              <>
                <div className="px-2.5 py-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--nv-text-tertiary)]">剧集管理</div>
                <MenuItem icon={<Image size={14} />} label="剧集海报" onClick={() => closeAndRun(onPosterPicker)} />
                <MenuItem icon={<Trash2 size={14} />} label="删除剧集" onClick={() => closeAndRun(onDelete)} danger />
                <div className="my-1 h-px bg-[var(--nv-border-subtle)]" />
              </>
            )}
            <MenuItem icon={<Share2 size={14} />} label="分享链接" onClick={() => closeAndRun(onShare)} />
          </div>
        )}
      </div>
    </>
  )

  return (
    <section
      className={`nv-detail-hero nv-series-hero relative overflow-hidden rounded-2xl border-b border-[var(--nv-border-subtle)] bg-[var(--nv-bg-canvas)]${showEpisodeSlideshow ? ' nv-series-hero-has-poster-slides' : ''}`}
      data-has-poster-slides={showEpisodeSlideshow ? 'true' : 'false'}
    >
      {showEpisodeSlideshow && (
        <div className="nv-series-poster-slideshow" aria-hidden="true">
          <img
            key={`series-poster-active-${series.id}-${posterShown}-${posterVersion}`}
            src={episodePosters[posterShown]}
            alt=""
            decoding="async"
            className={`nv-series-poster-slide is-active${isSwitching && nextReady ? ' is-leaving' : ''}`}
          />
          {isSwitching && (
            <img
              key={`series-poster-next-${series.id}-${posterSwitching}-${posterVersion}`}
              src={episodePosters[posterSwitching]}
              alt=""
              decoding="async"
              loading="eager"
              onLoad={() => setNextReady(true)}
              onTransitionEnd={handlePosterSwitchDone}
              className={`nv-series-poster-slide is-next${nextReady ? ' is-ready' : ''}`}
            />
          )}
          <div className="nv-series-poster-slideshow-scrim" />
        </div>
      )}
      <div className="nv-series-backdrop absolute inset-0 overflow-hidden" aria-hidden="true">
        {series.backdrop_path ? (
          <PosterImage
            key={`series-backdrop-${series.id}-${posterVersion}`}
            src={streamApi.getSeriesBackdropUrl(series.id, posterVersion)}
            alt=""
            className={`h-full w-full object-cover object-center transition-opacity duration-300 ${imageLoaded ? 'opacity-100' : 'opacity-0'}`}
            onLoad={() => setImageLoaded(true)}
          />
        ) : series.poster_path ? (
          <PosterImage
            key={`series-backdrop-poster-${series.id}-${posterVersion}`}
            src={streamApi.getSeriesPosterUrl(series.id, posterVersion)}
            alt=""
            className="h-full w-full scale-110 object-cover opacity-30 blur-2xl"
          />
        ) : null}
        <div className="nv-series-hero-reading-scrim absolute inset-0" />
        <div className="nv-series-hero-edge-scrim absolute inset-0" />
      </div>

      <div className="nv-detail-hero-inner nv-series-hero-inner relative mx-auto grid w-full max-w-[var(--nv-content-max)] items-center gap-6 px-[var(--nv-page-gutter)] sm:grid-cols-[12rem_minmax(0,1fr)] lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-8">
        <div className="hidden sm:block">
          <MediaArtwork
            src={series.poster_path ? streamApi.getSeriesPosterUrl(series.id, posterVersion) : null}
            alt={series.title}
            ratio="poster"
            loading="eager"
            fallback={<Tv size={32} aria-hidden="true" />}
            className="nv-detail-poster nv-series-poster shadow-[var(--nv-shadow-card)]"
          />
        </div>

        <HeroContent
          compact
          className="nv-series-hero-content"
          badges={(
            <>
              <Tag>剧集</Tag>
              {series.rating > 0 && <Tag tone="rating"><Star size={10} fill="currentColor" aria-hidden="true" /> {series.rating.toFixed(1)}</Tag>}
              {series.year > 0 && <Tag>{series.year}</Tag>}
            </>
          )}
          title={series.title}
          subtitle={series.orig_title && series.orig_title !== series.title ? series.orig_title : undefined}
          meta={genres.slice(0, 4).map((genre) => (
            <Link key={genre} to={`/search?q=${encodeURIComponent(genre)}`} className="hover:text-[var(--nv-text-primary)]">
              {genre}
            </Link>
          ))}
          overview={series.overview || undefined}
          actions={actions}
        />
      </div>
    </section>
  )
}

function MenuItem({ icon, label, onClick, danger = false }: { icon: ReactNode; label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`nv-menu-item ${danger ? '!text-[var(--nv-status-danger)]' : ''}`}
      role="menuitem"
    >
      {icon}
      <span>{label}</span>
    </button>
  )
}
