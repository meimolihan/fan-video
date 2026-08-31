import { useMemo } from 'react'
import { ArrowLeft, Calendar, Layers, Star } from 'lucide-react'
import { streamApi } from '@/api'
import type { CollectionWithMedia } from '@/types'
import { Button, Tag } from '@/components/design-system'
import PosterImage from '@/components/PosterImage'

interface CollectionDetailHeroProps {
  data: CollectionWithMedia
  movieCount: number
  onBack: () => void
}

export default function CollectionDetailHero({ data, movieCount, onBack }: CollectionDetailHeroProps) {
  const { collection, media } = data

  const stats = useMemo(() => {
    const years = media.filter((item) => item.year > 0).map((item) => item.year)
    const ratings = media.filter((item) => item.rating > 0).map((item) => item.rating)

    return {
      yearRange: years.length > 0 ? `${Math.min(...years)}${Math.min(...years) === Math.max(...years) ? '' : ` - ${Math.max(...years)}`}` : '',
      averageRating: ratings.length > 0 ? ratings.reduce((sum, value) => sum + value, 0) / ratings.length : 0,
    }
  }, [media])

  return (
    <section className="nv-collection-hero relative overflow-hidden border-b border-[var(--nv-border-subtle)]">
      <div className="nv-collection-hero-backdrop absolute inset-0 overflow-hidden" aria-hidden="true">
        <PosterImage
          src={streamApi.getCollectionPosterUrl(collection.id)}
          alt=""
          onError={(event) => { event.currentTarget.style.display = 'none' }}
        />
        <div className="absolute inset-0" style={{ background: 'var(--nv-hero-scrim)' }} />
        <div className="absolute inset-0" style={{ background: 'var(--nv-hero-bottom-scrim)' }} />
      </div>

      <div className="nv-collection-hero-inner relative">
        <Button type="button" variant="secondary" size="sm" onClick={onBack} className="nv-collection-hero-back">
          <ArrowLeft size={14} aria-hidden="true" />
          返回
        </Button>

        <div className="flex flex-col gap-6 sm:flex-row sm:items-end">
          <div className="nv-collection-hero-poster relative shrink-0 overflow-hidden">
            <div className="absolute inset-0 flex items-center justify-center text-[var(--nv-text-tertiary)]"><Layers size={42} aria-hidden="true" /></div>
            <PosterImage
              src={streamApi.getCollectionPosterUrl(collection.id)}
              alt={collection.name}
              className="relative h-full w-full object-cover"
              onError={(event) => { event.currentTarget.style.display = 'none' }}
            />
          </div>

          <div className="min-w-0 flex-1 pb-1">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <Tag tone="brand">系列合集</Tag>
              <Tag><span className="text-[var(--nv-status-warning)] font-bold">{movieCount}</span> 部视频</Tag>
              {stats.yearRange && <Tag><Calendar size={11} />{stats.yearRange}</Tag>}
              {stats.averageRating > 0 && <Tag tone="rating"><Star size={11} fill="currentColor" />均分 {stats.averageRating.toFixed(1)}</Tag>}
            </div>

            <h1 className="nv-collection-hero-title font-display text-[var(--nv-text-primary)]">
              {collection.name}
            </h1>

            {collection.overview && (
              <p className="mt-5 max-w-3xl text-sm leading-6 text-[var(--nv-text-secondary)]">{collection.overview}</p>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}
