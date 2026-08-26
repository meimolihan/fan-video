import { useEffect, useState, useRef, useCallback, type ImgHTMLAttributes } from 'react'

type PosterImageProps = Omit<ImgHTMLAttributes<HTMLImageElement>, 'src'> & {
  src: string
  thumbSrc?: string | null
}

/**
 * 海报/封面专用 <img>：渐进式加载
 * 1. 页面初始化先渲染缩略图低清占位（160px WebP），快速渲染页面骨架
 * 2. 使用 Intersection Observer 监听视口进入情况
 * 3. 视口进入后懒加载高清原图，原图加载完成后平滑替换，保留过渡效果
 *
 * 实现要点：
 * - 全程只有一个 <img> 元素，通过 src 切换 + opacity transition 实现平滑过渡
 * - thumbSrc / hdSrc / resolved 各司其职，无重复解析
 */
export default function PosterImage({ src, thumbSrc, ...rest }: PosterImageProps) {
  const [isInViewport, setIsInViewport] = useState(false)
  const [thumbReady, setThumbReady] = useState(false)
  const [hdReady, setHdReady] = useState(false)
  const imgRef = useRef<HTMLImageElement>(null)
  const hdPreloadRef = useRef<HTMLImageElement | null>(null)

  // IntersectionObserver：视口进入后触发高清图预加载
  useEffect(() => {
    if (!imgRef.current) return
    const el = imgRef.current

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) {
          setIsInViewport(true)
          observer.disconnect()
        }
      },
      { rootMargin: '300px 0px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  // 视口进入后用后台 <img> 预加载高清原图
  useEffect(() => {
    if (!isInViewport || hdReady) return

    const hdImg = new Image()
    hdPreloadRef.current = hdImg
    hdImg.src = src
    hdImg.onload = () => setHdReady(true)

    return () => {
      hdImg.onload = null
      hdPreloadRef.current = null
    }
  }, [isInViewport, src, hdReady])

  // 决定当前显示哪个 src + opacity
  const showHd = hdReady
  const showThumb = thumbSrc && !showHd
  const currentSrc = showHd ? src : thumbSrc || src

  // thumbSrc 存在时：先加载 thumb，等 hdReady 后切换 src
  // thumbSrc 不存在时：直接加载原图（由浏览器 lazy 控制）
  const handleLoad = useCallback(() => {
    if (showThumb) {
      setThumbReady(true)
    }
  }, [showThumb])

  // thumb 准备好（或无 thumb）→ 隐藏 fallback，显示图片
  const imageVisible = showThumb ? thumbReady : true

  return (
    <div
      className="nv-media-artwork"
      style={{
        width: rest.width || '100%',
        height: rest.height || '100%',
      }}
    >
      <img
        ref={imgRef}
        src={currentSrc}
        alt={rest.alt || ''}
        className={rest.className}
        loading="lazy"
        style={{
          opacity: imageVisible ? 1 : 0,
        }}
        onLoad={handleLoad}
        onError={() => {}}
      />
      {!imageVisible && (
        <div
          className="nv-media-artwork-fallback"
          style={{
            position: 'absolute',
            width: '100%',
            height: '100%',
            background: 'var(--nv-surface-elevated)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--nv-text-tertiary)',
            fontSize: 'var(--nv-font-xsmall)',
            pointerEvents: 'none',
          }}
          aria-hidden="true"
        >
          <svg
            width="40"
            height="60"
            viewBox="0 0 40 60"
            style={{ width: '20px', height: '30px' }}
            aria-hidden="true"
          >
            <rect width="40" height="60" fill="#1a1b2e" />
            <text
              fill="#86efac"
              font-family="Verdana"
              font-size="14"
              text-anchor="middle"
              x="20"
              y="38"
            >
              图
            </text>
          </svg>
          <span className="sr-only">海报加载中</span>
        </div>
      )}
    </div>
  )
}
