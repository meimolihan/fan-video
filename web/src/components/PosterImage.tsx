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
 * - 仅渲染 <img>，容器由 MediaArtwork 统一管理（避免双重 border-radius）
 */
export default function PosterImage({ src, thumbSrc, onError, ...rest }: PosterImageProps) {
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
    <img
      ref={imgRef}
      src={currentSrc}
      alt={rest.alt || ''}
      className={rest.className}
      loading="lazy"
      style={{
        opacity: imageVisible ? 1 : 0,
        width: rest.width || '100%',
        height: rest.height || '100%',
      }}
      onLoad={handleLoad}
      onError={onError}
    />
  )
}
