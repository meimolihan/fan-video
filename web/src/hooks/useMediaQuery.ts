import { useEffect, useState } from 'react'

/**
 * 响应式媒体查询 Hook。
 *
 * 返回当前视口是否匹配给定的媒体查询表达式，并在跨过断点时自动更新，
 * 供组件按断点切换渲染结构（例如桌面完整分页 / 手机紧凑分页）。
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  )

  useEffect(() => {
    const mediaQueryList = window.matchMedia(query)
    const handleChange = () => setMatches(mediaQueryList.matches)

    handleChange()
    mediaQueryList.addEventListener('change', handleChange)
    return () => mediaQueryList.removeEventListener('change', handleChange)
  }, [query])

  return matches
}
