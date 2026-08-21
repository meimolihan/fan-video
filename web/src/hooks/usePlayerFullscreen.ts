import { useCallback, useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import {
  FAKE_FULLSCREEN_CLASS,
  enterElementFullscreen,
  enterNativeVideoFullscreen,
  exitElementFullscreen,
  exitNativeVideoFullscreen,
  getFullscreenElement,
  isMobileDevice,
  lockOrientationLandscape,
  unlockOrientation,
} from '@/utils/fullscreen'

interface UsePlayerFullscreenOptions {
  /** 全屏目标容器 */
  containerRef: RefObject<HTMLElement | null>
  /** 视频元素（用于 iOS 原生全屏回退与状态同步，可选） */
  videoRef?: RefObject<HTMLVideoElement | null>
  /** 全屏状态变化回调（用于同步外部 store） */
  onFullscreenChange?: (isFullscreen: boolean) => void
}

/**
 * 播放器全屏 Hook：
 *   1. 标准全屏 API → webkit/moz/ms 前缀 → iOS <video> 原生全屏 → CSS 伪全屏兜底
 *   2. 移动端进入全屏时尝试锁定横屏，退出时解锁
 *   3. 监听 fullscreenchange / webkitfullscreenchange / webkitbegin(end)fullscreen 同步状态
 */
export function usePlayerFullscreen({ containerRef, videoRef, onFullscreenChange }: UsePlayerFullscreenOptions) {
  const [isFullscreen, setIsFullscreen] = useState(false)
  const fakeActiveRef = useRef(false)
  const onChangeRef = useRef(onFullscreenChange)

  useEffect(() => {
    onChangeRef.current = onFullscreenChange
  })

  const syncState = useCallback((value: boolean) => {
    setIsFullscreen(value)
    onChangeRef.current?.(value)
  }, [])

  useEffect(() => {
    const handleChange = () => {
      if (fakeActiveRef.current) return
      syncState(!!getFullscreenElement())
    }
    document.addEventListener('fullscreenchange', handleChange)
    document.addEventListener('webkitfullscreenchange', handleChange)

    // iPhone Safari 原生视频全屏事件
    const video = videoRef?.current ?? null
    const handleBegin = () => syncState(true)
    const handleEnd = () => syncState(false)
    video?.addEventListener('webkitbeginfullscreen', handleBegin)
    video?.addEventListener('webkitendfullscreen', handleEnd)

    return () => {
      document.removeEventListener('fullscreenchange', handleChange)
      document.removeEventListener('webkitfullscreenchange', handleChange)
      video?.removeEventListener('webkitbeginfullscreen', handleBegin)
      video?.removeEventListener('webkitendfullscreen', handleEnd)
    }
  }, [videoRef, syncState])

  const enterFullscreen = useCallback(async () => {
    const container = containerRef.current
    if (!container) return

    if (await enterElementFullscreen(container)) {
      if (isMobileDevice()) await lockOrientationLandscape()
      return
    }

    // iPhone Safari：元素级全屏不可用，退回 <video> 原生全屏
    if (enterNativeVideoFullscreen(videoRef?.current ?? null)) return

    // 最终兜底：CSS 伪全屏（无任何全屏 API 的环境）
    fakeActiveRef.current = true
    container.classList.add(FAKE_FULLSCREEN_CLASS)
    syncState(true)
  }, [containerRef, videoRef, syncState])

  const exitFullscreen = useCallback(async () => {
    if (fakeActiveRef.current) {
      fakeActiveRef.current = false
      containerRef.current?.classList.remove(FAKE_FULLSCREEN_CLASS)
      unlockOrientation()
      syncState(false)
      return
    }
    if (!(await exitElementFullscreen())) {
      exitNativeVideoFullscreen(videoRef?.current ?? null)
    }
    unlockOrientation()
  }, [containerRef, videoRef, syncState])

  const toggleFullscreen = useCallback(() => {
    if (getFullscreenElement() || fakeActiveRef.current) exitFullscreen()
    else enterFullscreen()
  }, [enterFullscreen, exitFullscreen])

  return { isFullscreen, toggleFullscreen }
}
