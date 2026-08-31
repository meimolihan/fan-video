import { useCallback, useEffect, useRef, useState } from 'react'
import { ImageOff, Plus, RefreshCw, Search, Sparkles, Trash2 } from 'lucide-react'
import { AdminPanel } from '@/components/admin/AdminPrimitives'
import { Button } from '@/components/design-system'
import { useToast } from '@/components/Toast'
import { homeApi, mediaApi, streamApi, type HomeFeaturedEntry } from '@/api'
import { usePosterVersion } from '@/stores/mediaRefresh'
import type { Media, Series } from '@/types'

const MIN_ITEMS = 2

function EntryThumb({ entry, posterVersion }: { entry: HomeFeaturedEntry; posterVersion?: number }) {
  const [failed, setFailed] = useState(false)
  const [useBackdrop, setUseBackdrop] = useState(true)

  const movie = entry.item_type === 'movie'
  const backdrop = movie
    ? streamApi.getBackdropUrl(entry.item_id, posterVersion)
    : streamApi.getSeriesBackdropUrl(entry.item_id, posterVersion)
  const poster = movie
    ? streamApi.getPosterUrl(entry.item_id, posterVersion)
    : streamApi.getSeriesPosterUrl(entry.item_id, posterVersion)

  if (failed || !entry.valid) {
    return (
      <div className="flex h-10 w-[72px] shrink-0 items-center justify-center rounded-[var(--nv-radius-control)] bg-[var(--nv-bg-hover)] text-[var(--nv-text-tertiary)]">
        <ImageOff size={14} aria-hidden="true" />
      </div>
    )
  }
  return (
    <img
      src={useBackdrop ? backdrop : poster}
      alt=""
      loading="lazy"
      className="h-10 w-[72px] shrink-0 rounded-[var(--nv-radius-control)] object-cover"
      onError={() => {
        if (useBackdrop) setUseBackdrop(false)
        else setFailed(true)
      }}
    />
  )
}

export default function HomeFeaturedPanel() {
  const toast = useToast()
  const posterVersion = usePosterVersion()
  const [entries, setEntries] = useState<HomeFeaturedEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<string | null>(null)

  // 添加搜索
  const [keyword, setKeyword] = useState('')
  const [searching, setSearching] = useState(false)
  const [results, setResults] = useState<{
    movies: Media[]
    series: Series[]
    /** 分集命中作为独立视频条目展示，可直接添加（movie 类型） */
    episodes: Media[]
  }>({ movies: [], series: [], episodes: [] })
  const [addedIds, setAddedIds] = useState<Set<string>>(new Set())
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const active = entries.length >= MIN_ITEMS

  const loadEntries = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await homeApi.listFeatured()
      setEntries(data.data || [])
      setAddedIds(new Set((data.data || []).map((entry) => `${entry.item_type}:${entry.item_id}`)))
    } catch (error) {
      toast.error(`读取精选轮播失败：${error instanceof Error ? error.message : String(error)}`)
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => { loadEntries() }, [loadEntries])

  useEffect(() => () => {
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
  }, [])

  const runSearch = useCallback(async (query: string) => {
    if (!query.trim()) {
      setResults({ movies: [], series: [], episodes: [] })
      return
    }
    setSearching(true)
    try {
      const { data } = await mediaApi.searchMixed(query.trim(), 1, 20)
      const allMedia = data.media || []
      const seriesResults = data.series || []

      // 分集作为独立视频条目展示，添加时直接引用该文件本身（movie 类型）
      setResults({
        movies: allMedia.filter((media) => media.media_type !== 'episode'),
        series: seriesResults,
        episodes: allMedia.filter((media) => media.media_type === 'episode'),
      })
    } catch {
      setResults({ movies: [], series: [], episodes: [] })
    } finally {
      setSearching(false)
    }
  }, [])

  const handleKeywordChange = (value: string) => {
    setKeyword(value)
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    searchTimerRef.current = setTimeout(() => runSearch(value), 400)
  }

  const handleAdd = async (itemType: 'movie' | 'series', itemId: string) => {
    try {
      const { data } = await homeApi.addFeatured(itemType, itemId)
      await loadEntries()
      toast[data.active ? 'success' : 'info'](
        data.active ? '精选已生效，首页轮播将优先展示' : `已添加（当前 ${data.total} 项，满 ${MIN_ITEMS} 项生效）`,
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    }
  }

  const handleRemove = async (entry: HomeFeaturedEntry) => {
    setBusyId(entry.id)
    try {
      const { data } = await homeApi.removeFeatured(entry.id)
      setEntries((list) => list.filter((item) => item.id !== entry.id))
      setAddedIds((prev) => {
        const next = new Set(prev)
        next.delete(`${entry.item_type}:${entry.item_id}`)
        return next
      })
      toast.info(
        data.active
          ? '已移除，精选继续生效'
          : `已移除（剩余 ${data.total} 项，不足 ${MIN_ITEMS} 项时使用默认推荐）`,
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    } finally {
      setBusyId(null)
    }
  }

  return (
    <AdminPanel
      title="首页轮播精选"
      description={`手动指定首页顶部轮播内容。添加满 ${MIN_ITEMS} 个后生效并优先于智能推荐；不足 ${MIN_ITEMS} 个时仍使用默认推荐逻辑。`}
      icon={<Sparkles size={16} aria-hidden="true" />}
      actions={(
        <>
          <span className="whitespace-nowrap text-sm tabular-nums text-[var(--nv-text-secondary)]">
            精选 <b className="text-[var(--nv-text-primary)]">{entries.length}</b> 项
            {' · '}
            {active
              ? <span className="text-[var(--nv-status-success)]">已生效</span>
              : <span className="text-[var(--nv-text-tertiary)]">未生效</span>}
          </span>
          <Button variant="ghost" size="sm" onClick={loadEntries} disabled={loading} aria-label="刷新精选列表">
            <RefreshCw size={15} className={loading ? 'animate-spin' : ''} aria-hidden="true" />
            刷新
          </Button>
        </>
      )}
    >
      {/* 搜索添加 */}
      <div className="mb-4">
        <div className="relative">
          <Search size={15} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--nv-text-tertiary)]" />
          <input
            type="search"
            value={keyword}
            onChange={(event) => handleKeywordChange(event.target.value)}
            placeholder="搜索视频、剧集或单集名称，点击结果添加到轮播…"
            aria-label="搜索媒体以添加精选"
            className="w-full rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] bg-[var(--nv-bg-input)] py-2 pl-9 pr-3 text-sm text-[var(--nv-text-primary)] placeholder:text-[var(--nv-text-tertiary)] focus:border-[var(--nv-border-focus)] focus:outline-none"
          />
        </div>

        {(results.movies.length > 0 || results.series.length > 0 || results.episodes.length > 0) && (
          <ul className="mt-2 max-h-64 space-y-1.5 overflow-y-auto rounded-[var(--nv-radius-control)] border border-[var(--nv-border)] p-2">
            {results.movies.map((media) => {
              const key = `movie:${media.id}`
              return (
                <li key={key} className="flex items-center gap-3 rounded-[var(--nv-radius-control)] px-2 py-1.5 hover:bg-[var(--nv-bg-hover)]">
                  <span className="shrink-0 rounded bg-[var(--nv-bg-hover)] px-1.5 py-0.5 text-xs text-[var(--nv-text-tertiary)]">视频</span>
                  <span className="min-w-0 flex-1 truncate text-sm text-[var(--nv-text-primary)]" title={media.title}>
                    {media.title}{media.year ? ` (${media.year})` : ''}
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={addedIds.has(key)}
                    onClick={() => handleAdd('movie', media.id)}
                  >
                    {addedIds.has(key) ? '已添加' : (<><Plus size={13} aria-hidden="true" /> 添加</>)}
                  </Button>
                </li>
              )
            })}
            {results.series.map((series) => {
              const key = `series:${series.id}`
              return (
                <li key={key} className="flex items-center gap-3 rounded-[var(--nv-radius-control)] px-2 py-1.5 hover:bg-[var(--nv-bg-hover)]">
                  <span className="shrink-0 rounded bg-[var(--nv-bg-hover)] px-1.5 py-0.5 text-xs text-[var(--nv-text-tertiary)]">剧集</span>
                  <span className="min-w-0 flex-1 truncate text-sm text-[var(--nv-text-primary)]" title={series.title}>
                    {series.title}{series.year ? ` (${series.year})` : ''}
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={addedIds.has(key)}
                    onClick={() => handleAdd('series', series.id)}
                  >
                    {addedIds.has(key) ? '已添加' : (<><Plus size={13} aria-hidden="true" /> 添加</>)}
                  </Button>
                </li>
              )
            })}
            {results.episodes.map((media) => {
              const key = `movie:${media.id}`
              return (
                <li key={key} className="flex items-center gap-3 rounded-[var(--nv-radius-control)] px-2 py-1.5 hover:bg-[var(--nv-bg-hover)]">
                  <span className="shrink-0 rounded bg-[var(--nv-bg-hover)] px-1.5 py-0.5 text-xs text-[var(--nv-text-tertiary)]">单集</span>
                  <span className="min-w-0 flex-1 truncate text-sm text-[var(--nv-text-primary)]">
                    <span title={media.title}>{media.title}</span>
                    {media.series?.title && <span className="ml-1 text-xs text-[var(--nv-text-tertiary)]">（{media.series.title}）</span>}
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={addedIds.has(key)}
                    onClick={() => handleAdd('movie', media.id)}
                  >
                    {addedIds.has(key) ? '已添加' : (<><Plus size={13} aria-hidden="true" /> 添加视频</>)}
                  </Button>
                </li>
              )
            })}
          </ul>
        )}
        {searching && (
          <p className="mt-2 text-xs text-[var(--nv-text-tertiary)]">搜索中…</p>
        )}
      </div>

      {/* 当前精选列表 */}
      {loading ? (
        <p className="py-4 text-center text-sm text-[var(--nv-text-tertiary)]">加载中…</p>
      ) : entries.length === 0 ? (
        <p className="rounded-[var(--nv-radius-control)] border border-dashed border-[var(--nv-border)] px-4 py-6 text-center text-sm text-[var(--nv-text-tertiary)]">
          还没有手动精选条目，首页轮播当前由智能推荐驱动。搜索并添加至少 {MIN_ITEMS} 个条目即可接管。
        </p>
      ) : (
        <ol className="space-y-1.5">
          {entries.map((entry, index) => (
            <li
              key={entry.id}
              className={`flex items-center gap-3 rounded-[var(--nv-radius-control)] border px-3 py-2 ${
                entry.valid ? 'border-[var(--nv-border)]' : 'border-dashed border-[var(--nv-status-danger)] opacity-70'
              }`}
            >
              <span className="w-5 shrink-0 text-center text-xs tabular-nums text-[var(--nv-text-tertiary)]">{index + 1}</span>
              <EntryThumb entry={entry} posterVersion={posterVersion} />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-[var(--nv-text-primary)]" title={entry.title}>{entry.title}</p>
                <p className="mt-0.5 truncate text-xs text-[var(--nv-text-tertiary)]">
                  {entry.item_type === 'series' ? '剧集' : entry.kind === 'episode' ? '单集' : '视频'}
                  {entry.year ? ` · ${entry.year}` : ''}
                  {!entry.valid && ' · 引用已失效，展示时自动跳过'}
                </p>
              </div>
              <Button
                size="sm"
                variant="ghost"
                iconOnly
                disabled={busyId === entry.id}
                onClick={() => handleRemove(entry)}
                aria-label={`移除 ${entry.title}`}
                title="移除"
              >
                <Trash2 size={15} aria-hidden="true" className="text-[var(--nv-status-danger)]" />
              </Button>
            </li>
          ))}
        </ol>
      )}
    </AdminPanel>
  )
}
