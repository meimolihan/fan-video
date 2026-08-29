import { useEffect, useState, useMemo, type HTMLAttributes, type ReactNode } from 'react'
import clsx from 'clsx'
import { Film } from 'lucide-react'
import { resolvePosterUrl } from '@/utils/posterCache'
import PosterImage from '@/components/PosterImage'

export type MediaArtworkRatio = 'poster' | 'landscape' | 'hero' | 'square'

export interface MediaArtworkProps extends HTMLAttributes<HTMLDivElement> {
  src?: string | null
  fallbackSrc?: string | null
  alt?: string
  ratio?: MediaArtworkRatio
  loading?: 'eager' | 'lazy'
  fallback?: ReactNode
  imageClassName?: string
  overlay?: ReactNode
}

function useResolvedArtworkUrl(src: string | null | undefined) {
  const [state, setState] = useState<{ ready: boolean; url: string | null | undefined }>(
    () => (src ? { ready: false, url: null } : { ready: true, url: src }),
  )

  useEffect(() => {
    if (!src) {
      setState({ ready: true, url: src })
      return
    }
    let alive = true
    setState({ ready: false, url: null })
    // 解析期间不把原始 URL 交给 <img>：否则浏览器会照常发起网络请求，
    // 缓存就失去意义了。解析结果要么是本地 blob URL，要么回退原始地址。
    resolvePosterUrl(src).then((url) => {
      if (alive) setState({ ready: true, url })
    })
    return () => {
      alive = false
    }
  }, [src])

  return state
}

/**
 * 从海报 URL 推导缩略图 URL。
 * 海报格式：/api/media/{id}/poster 或 /api/media/{id}/poster?token=xxx
 * 缩略图格式：/api/media/{id}/poster/thumb 或 /api/media/{id}/poster/thumb?token=xxx
 * 也兼容 /api/series/{id}/poster 和 /api/collections/{id}/poster
 */


export function MediaArtwork({
  src,
  fallbackSrc,
  alt = '',
  ratio = 'poster',
  loading = 'lazy',
  fallback,
  imageClassName,
  overlay,
  className,
  children,
  ...props
}: MediaArtworkProps) {
  const primary = useResolvedArtworkUrl(src)
  const [fallbackResolved, setFallbackResolved] = useState<string | null>(null)
  const [primaryBroken, setPrimaryBroken] = useState(false)

  useEffect(() => {
    if (fallbackSrc) {
      resolvePosterUrl(fallbackSrc).then((u) => setFallbackResolved(u ?? fallbackSrc))
    }
  }, [fallbackSrc])

  // 主图源变化时重置错误状态，重新尝试主图
  useEffect(() => {
    setPrimaryBroken(false)
  }, [src])

  const primaryUrl = primary.ready && primary.url ? primary.url : undefined
  const activeSrc = !primaryBroken && primaryUrl ? primaryUrl : fallbackResolved

  // 主图加载失败（如 backdrop 端点 404 返回占位/错误）时回退到备用图，
  // 避免 <img> 显示浏览器原生破图「?」。
  const handlePosterError = () => {
    if (!primaryBroken && fallbackResolved && fallbackResolved !== primaryUrl) {
      setPrimaryBroken(true)
    }
  }

  // 从原始 src（非缓存 blob）推导缩略图 URL
  const thumbSrc = useMemo(() => {
    if (!src && !fallbackSrc) return null
    const candidate = src ?? fallbackSrc
    if (!candidate) return null
    const match = candidate.match(/(\/poster)(\?.*)?$/)
    if (match) return candidate.replace(/(\/poster)(\?.*)?$/, '$1/thumb$2')
    return null
  }, [src, fallbackSrc])

  const effectiveSrc = activeSrc

  return (
    <div
      {...props}
      className={clsx('nv-media-artwork', className)}
      data-ratio={ratio}
      data-image-state={effectiveSrc ? 'ready' : 'fallback'}
    >
      {effectiveSrc ? (
        <PosterImage
          src={effectiveSrc}
          thumbSrc={thumbSrc}
          alt={alt}
          className={clsx('nv-media-artwork-image', imageClassName)}
          loading={loading}
          onError={handlePosterError}
        />
      ) : (
        <div className="nv-media-artwork-fallback" aria-hidden={alt ? undefined : true}>
          {fallback ?? <Film size={24} aria-hidden="true" />}
        </div>
      )}
      {overlay && <div className="nv-media-artwork-overlay">{overlay}</div>}
      {children}
    </div>
  )
}

export default MediaArtwork