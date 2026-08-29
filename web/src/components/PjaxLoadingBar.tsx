import { useEffect, useState } from 'react'
import { usePjaxStore } from '@/stores/pjax'

/**
 * 全局 PJAX 加载指示条。
 *
 * 挂在视口最顶端的一条细进度条 + 一处极淡的整页蒙层，用于在路由切换期间
 * 给出“局部刷新进行中”的视觉反馈。蒙层 pointer-events: none，不拦截点击，
 * 侧边栏/导航保持可交互，符合“切换只替换内容容器、保留导航侧边栏”的意图。
 * 加载结束后自动淡出；真正的整页降级刷新由 PjaxBridge 依据 X-PJAX 预取结果执行。
 */
export default function PjaxLoadingBar() {
  const loading = usePjaxStore((s) => s.loading)
  const [visible, setVisible] = useState(false)
  const [removed, setRemoved] = useState(true)

  useEffect(() => {
    if (loading) {
      setRemoved(false)
      setVisible(false)
      // 延迟数十毫秒再显示，避免极快切换时进度条闪烁。
      const show = window.setTimeout(() => setVisible(true), 120)
      return () => {
        window.clearTimeout(show)
        setRemoved(true)
      }
    }

    setVisible(false)
    const hide = window.setTimeout(() => setRemoved(true), 320)
    return () => window.clearTimeout(hide)
  }, [loading])

  if (removed) return null

  return (
    <div aria-hidden="true" className="nv-pjax-layer" data-pjax-visible={visible ? 'true' : undefined}>
      <div className="nv-pjax-backdrop" />
      <div className="nv-pjax-bar" />
    </div>
  )
}
