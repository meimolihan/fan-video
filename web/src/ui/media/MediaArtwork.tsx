import { useEffect, useState, type HTMLAttributes, type ReactNode } from 'react'
import clsx from 'clsx'
import { Film } from 'lucide-react'
import { resolvePosterUrl } from '@/utils/posterCache'

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

/** 应用级海报缓存解析：命中 IndexedDB 时返回本地 objectURL（秒出、零网络） */
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
    // 解析期间不把原始 URL 交给 <img>：否则浏览器会照常发起网络请求，
    // 缓存就失去意义了。解析结果要么是本地 blob URL，要么回退原始地址。
    setState({ ready: false, url: null })
    resolvePosterUrl(src).then((url) => {
      if (alive) setState({ ready: true, url })
    })
    return () => {
      alive = false
    }
  }, [src])

  return state
}

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
  const [usingFallbackSrc, setUsingFallbackSrc] = useState(false)
  const [failed, setFailed] = useState(false)
  const primary = useResolvedArtworkUrl(src)
  const alternate = useResolvedArtworkUrl(fallbackSrc)

  useEffect(() => {
    setUsingFallbackSrc(false)
    setFailed(false)
  }, [fallbackSrc, src])

  const activeSrc = usingFallbackSrc
    ? (alternate.ready ? (alternate.url ?? fallbackSrc) : null)
    : (primary.ready ? (primary.url ?? src) : null)
  const showImage = Boolean(activeSrc) && !failed

  const handleImageError = () => {
    if (!usingFallbackSrc && fallbackSrc && fallbackSrc !== src) {
      setUsingFallbackSrc(true)
      setFailed(false)
      return
    }
    setFailed(true)
  }

  return (
    <div
      {...props}
      className={clsx('nv-media-artwork', className)}
      data-ratio={ratio}
      data-image-state={showImage ? (usingFallbackSrc ? 'fallback-image' : 'ready') : 'fallback'}
    >
      {showImage ? (
        <img
          src={activeSrc!}
          alt={alt}
          loading={loading}
          className={clsx('nv-media-artwork-image', imageClassName)}
          onLoad={() => setFailed(false)}
          onError={handleImageError}
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
