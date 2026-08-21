import { useEffect } from 'react'

/**
 * 播放时把当前时间写入浏览器标签页/地址栏标题，
 * 方便移动端全屏或后台播放时查看时间；暂停或离开页面时恢复原标题。
 */
export function useTitleClock(baseTitle: string, enabled: boolean) {
  useEffect(() => {
    if (!enabled) return

    const formatClock = () => {
      const now = new Date()
      return `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
    }

    let lastClock = formatClock()
    document.title = `${lastClock} · ${baseTitle}`

    const timer = window.setInterval(() => {
      const clock = formatClock()
      if (clock !== lastClock) {
        lastClock = clock
        document.title = `${clock} · ${baseTitle}`
      }
    }, 1000)

    return () => {
      window.clearInterval(timer)
      document.title = baseTitle
    }
  }, [baseTitle, enabled])
}
