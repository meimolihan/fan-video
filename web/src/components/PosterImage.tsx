import { useEffect, useState, type ImgHTMLAttributes } from 'react'
import { resolvePosterUrl } from '@/utils/posterCache'

type PosterImageProps = Omit<ImgHTMLAttributes<HTMLImageElement>, 'src'> & {
  src: string
}

/**
 * 海报/封面专用 <img>：
 * 通过应用级 IndexedDB 缓存解析地址（命中时本地 objectURL 秒出、零网络）。
 * 解析完成前不渲染 <img>——否则浏览器会照常请求原始 URL，缓存形同虚设。
 * 未命中或缓存不可用时回退原始 URL，行为等同普通 <img>。
 */
export default function PosterImage({ src, ...rest }: PosterImageProps) {
  const [resolved, setResolved] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    setResolved(null)
    resolvePosterUrl(src).then((url) => {
      if (alive) setResolved(url)
    })
    return () => {
      alive = false
    }
  }, [src])

  if (!resolved) return null
  return <img {...rest} src={resolved} />
}
