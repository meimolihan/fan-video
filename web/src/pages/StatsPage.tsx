import { useEffect, useMemo, useState } from 'react'
import { BarChart3, Clock, Film, Heart, Trash2 } from 'lucide-react'
import { statsApi, streamApi } from '@/api'
import { useTranslation } from '@/i18n'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import type { UserStatsOverview } from '@/types'
import PosterImage from '@/components/PosterImage'
import { EmptyState, PageContainer, Section, Tag, Button } from '@/components/design-system'
import { PersonalWorkspaceHeader } from '@/ui'

export default function StatsPage() {
  const [stats, setStats] = useState<UserStatsOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const { t } = useTranslation()
  const toast = useToast()
  const dialog = useDialog()

  useEffect(() => {
    const fetchStats = async () => {
      try {
        const res = await statsApi.getMyStats()
        setStats(res.data.data)
      } catch {
        // 保持原有静默失败行为。
      } finally {
        setLoading(false)
      }
    }
    fetchStats()
  }, [])

  const hasData = !!stats && (
    stats.total_minutes > 0
    || (stats.daily_stats?.length ?? 0) > 0
    || (stats.top_genres?.length ?? 0) > 0
    || (stats.most_watched?.length ?? 0) > 0
  )

  const handleClear = async () => {
    const ok = await dialog.confirm({
      title: t('stats.clearConfirmTitle'),
      message: t('stats.clearConfirm'),
      confirmText: t('stats.clear'),
      variant: 'danger',
    })
    if (!ok) return
    try {
      await statsApi.clearMyStats()
      setStats(null)
      toast.success(t('stats.cleared'))
    } catch {
      toast.error(t('stats.clearFailed'))
    }
  }

  const dailyMax = useMemo(() => {
    if (!stats?.daily_stats?.length) return 0
    return Math.max(...stats.daily_stats.map((day) => Number(day.total_minutes) || 0))
  }, [stats?.daily_stats])

  if (loading) {
    return (
      <PageContainer>
        <div className="space-y-8 animate-pulse">
          <div className="border-b border-[var(--nv-border-subtle)] pb-5">
            <div className="h-7 w-40 rounded-[var(--nv-radius-control)] bg-[var(--nv-bg-interactive)]" />
            <div className="mt-2 h-3.5 w-64 rounded-[var(--nv-radius-control)] bg-[var(--nv-bg-interactive)]" />
          </div>
          <div className="grid border-y border-[var(--nv-border-subtle)] sm:grid-cols-2 xl:grid-cols-4">
            {[0, 1, 2, 3].map((index) => (
              <div key={index} className={`flex min-w-0 items-start gap-3 px-1 py-4 sm:px-4 ${index > 0 ? 'border-t border-[var(--nv-border-subtle)] sm:border-t-0' : ''} ${index % 2 !== 0 ? 'sm:border-l sm:border-[var(--nv-border-subtle)]' : ''} ${index >= 2 ? 'sm:border-t sm:border-[var(--nv-border-subtle)] xl:border-t-0' : ''} ${index > 0 ? 'xl:border-l xl:border-[var(--nv-border-subtle)]' : ''}`}>
                <div className="h-4 w-4 shrink-0 rounded bg-[var(--nv-bg-interactive)]" />
                <div className="w-full min-w-0 space-y-2">
                  <div className="h-2.5 w-20 rounded bg-[var(--nv-bg-interactive)]" />
                  <div className="h-5 w-28 rounded-[var(--nv-radius-control)] bg-[var(--nv-bg-interactive)]" />
                  <div className="h-2.5 w-16 rounded bg-[var(--nv-bg-interactive)]" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </PageContainer>
    )
  }

  if (!stats) {
    return (
      <PageContainer>
        <EmptyState icon={<BarChart3 size={28} />} title={t('stats.noData')} description={t('stats.noDataHint')} />
      </PageContainer>
    )
  }

  const statItems = [
    {
      icon: Clock,
      label: t('stats.totalWatchTime'),
      value: t('stats.hours', { hours: Number(stats.total_hours ?? 0).toFixed(1) }),
      subValue: t('stats.minutes', { minutes: Number(stats.total_minutes ?? 0).toFixed(0) }),
    },
    {
      icon: Film,
      label: t('stats.watchedCount'),
      value: t('stats.countUnit', { count: String(stats.most_watched?.length || 0) }),
      subValue: t('stats.growing'),
    },
    {
      icon: Heart,
      label: t('stats.favoriteGenre'),
      value: stats.top_genres?.[0]?.genres?.split(',')[0] || t('stats.noGenre'),
      subValue: stats.top_genres?.[0] ? t('stats.minutes', { minutes: Number(stats.top_genres[0].total_minutes).toFixed(0) }) : '',
    },
    {
      icon: BarChart3,
      label: t('stats.dailyAvg'),
      value: stats.daily_stats?.length
        ? t('stats.dailyAvgMinutes', { minutes: (Number(stats.total_minutes ?? 0) / Math.max(stats.daily_stats.length, 1)).toFixed(0) })
        : t('stats.dailyAvgMinutes', { minutes: '0' }),
      subValue: t('stats.last30Days'),
    },
  ]

  return (
    <PageContainer>
      <div className="space-y-8">
        <PersonalWorkspaceHeader
          icon={<BarChart3 size={20} />}
          title={t('stats.title')}
          description="观看时长、内容偏好与最近观看趋势。"
          actions={hasData ? (
            <Button variant="danger" size="sm" onClick={handleClear} className="shrink-0">
              <Trash2 size={14} aria-hidden="true" />
              {t('stats.clearAll')}
            </Button>
          ) : undefined}
        />

        <section aria-label={t('stats.title')} className="grid border-y border-[var(--nv-border-subtle)] sm:grid-cols-2 xl:grid-cols-4">
          {statItems.map(({ icon: Icon, label, value, subValue }, index) => (
            <div
              key={label}
              className={`flex min-w-0 items-start gap-3 px-1 py-4 sm:px-4 ${index > 0 ? 'border-t border-[var(--nv-border-subtle)] sm:border-t-0' : ''} ${index % 2 !== 0 ? 'sm:border-l sm:border-[var(--nv-border-subtle)]' : ''} ${index >= 2 ? 'sm:border-t sm:border-[var(--nv-border-subtle)] xl:border-t-0' : ''} ${index > 0 ? 'xl:border-l xl:border-[var(--nv-border-subtle)]' : ''}`}
            >
              <Icon size={16} className="mt-0.5 shrink-0 text-[var(--nv-text-tertiary)]" aria-hidden="true" />
              <div className="min-w-0">
                <p className="text-[11px] font-medium uppercase tracking-[0.08em] text-[var(--nv-text-tertiary)]">{label}</p>
                <p className="mt-1.5 truncate text-xl font-semibold tracking-[-0.02em] text-[var(--nv-text-primary)]">{value}</p>
                {subValue && <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">{subValue}</p>}
              </div>
            </div>
          ))}
        </section>

        {stats.daily_stats && stats.daily_stats.length > 0 && (
          <Section title={t('stats.dailyTrend')} description={t('stats.last30Days')}>
            <div className="flex min-h-40 items-end gap-2 overflow-x-auto border-y border-[var(--nv-border-subtle)] py-4">
              {stats.daily_stats.map((day) => {
                const minutes = Number(day.total_minutes) || 0
                const height = dailyMax > 0 ? (minutes / dailyMax) * 112 : 0
                return (
                  <div key={day.date} className="group flex min-w-7 flex-1 flex-col items-center justify-end gap-2" title={`${day.date}: ${minutes.toFixed(0)} min`}>
                    <span className="text-[10px] text-[var(--nv-text-tertiary)] opacity-0 transition-opacity duration-150 group-hover:opacity-100">{minutes.toFixed(0)}m</span>
                    <div
                      className="w-full max-w-7 rounded-t-[var(--nv-radius-sm)] bg-[var(--nv-text-secondary)] opacity-55 transition-opacity duration-150 group-hover:opacity-85"
                      style={{ height: `${Math.max(height, 4)}px` }}
                    />
                    <span className="text-[10px] text-[var(--nv-text-tertiary)]">{day.date.slice(5)}</span>
                  </div>
                )
              })}
            </div>
          </Section>
        )}

        {stats.top_genres && stats.top_genres.length > 0 && (
          <Section title={t('stats.topGenres')}>
            <div className="divide-y divide-[var(--nv-border-subtle)] border-y border-[var(--nv-border-subtle)]">
              {stats.top_genres.map((genre, index) => {
                const maxMinutes = Number(stats.top_genres?.[0]?.total_minutes) || 1
                const minutes = Number(genre.total_minutes) || 0
                const percentage = Math.min(100, (minutes / maxMinutes) * 100)
                const name = String(genre.genres || '').split(',')[0]
                return (
                  <div key={`${name}-${index}`} className="grid grid-cols-[minmax(5rem,8rem)_1fr_auto] items-center gap-3 px-1 py-3">
                    <span className="truncate text-sm font-medium text-[var(--nv-text-primary)]">{name}</span>
                    <div className="h-1 overflow-hidden rounded-full bg-[var(--nv-fill-hover)]">
                      <div className="h-full rounded-full bg-[var(--nv-text-secondary)] opacity-70" style={{ width: `${percentage}%` }} />
                    </div>
                    <span className="min-w-16 text-right text-xs text-[var(--nv-text-tertiary)]">{minutes.toFixed(0)}min</span>
                  </div>
                )
              })}
            </div>
          </Section>
        )}

        {stats.most_watched && stats.most_watched.length > 0 && (
          <Section title={t('stats.mostWatched')}>
            <div className="grid grid-cols-2 gap-x-4 gap-y-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
              {stats.most_watched.map((item) => (
                <article key={item.media_id} className="group min-w-0 transition-transform duration-150 hover:-translate-y-0.5">
                  <div className="relative aspect-[2/3] overflow-hidden rounded-[var(--nv-radius-card)] border border-[var(--nv-border-subtle)] bg-[var(--nv-bg-surface-soft)] shadow-[var(--nv-shadow-card)] transition-[box-shadow,border-color] duration-150 group-hover:border-[var(--nv-border-default)] group-hover:shadow-[var(--nv-shadow-card-hover)]">
                    {item.poster_path ? (
                      <PosterImage
                        src={item.media_type === 'series'
                          ? streamApi.getSeriesPosterUrl(item.media_id)
                          : streamApi.getPosterUrl(item.media_id)}
                        alt={String(item.title)}
                        className="h-full w-full object-cover"
                        loading="lazy"
                      />
                    ) : (
                      <div className="flex h-full items-center justify-center text-[var(--nv-text-tertiary)]"><Film size={30} aria-hidden="true" /></div>
                    )}
                    <div className="absolute bottom-2 right-2">
                      <Tag>{t('stats.minutes', { minutes: Number(item.total_minutes).toFixed(0) })}</Tag>
                    </div>
                  </div>
                  <h3 className="mt-2.5 truncate px-0.5 text-sm font-medium text-[var(--nv-text-primary)]" title={String(item.title)}>{String(item.title)}</h3>
                </article>
              ))}
            </div>
          </Section>
        )}
      </div>
    </PageContainer>
  )
}
