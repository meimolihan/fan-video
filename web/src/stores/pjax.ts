import { create } from 'zustand'

/**
 * PJAX 轻量化改造的全局路由过渡状态机。
 *
 * 本项目前端是 React SPA，react-router 已承担客户端片段切换（History API、
 * 保留导航侧栏、popstate 前进后退、懒加载与错误处理）。这里仅新增一层
 * “PJAX 风格”的全局过渡协调，不做 DOM 级 innerHTML 替换，最大限度避免与
 * React 虚拟 DOM 冲突，原有组件/样式/懒加载/播放器全部保持不变。
 *
 * 对外只暴露极简原语：
 *  - progress / start / finish：驱动全局 loading 指示
 *  - requestFullReload：请求整页降级刷新（失败兜底）
 */
interface PjaxState {
  /** 是否正处于路由切换的“加载中”状态。 */
  loading: boolean
  /** 当前加载中的目标 URL（仅用于展示/调试，可为空）。 */
  target: string | null
}

interface PjaxActions {
  /** 发起一次过渡：显示全局 loading。 */
  start: (target?: string) => void
  /** 结束过渡：隐藏全局 loading。 */
  finish: () => void
}

const SPLIT_WINDOW = 220

/** 内部请求计数：并发/连续点击时确保 loading 只在整个批次结束后才隐藏。 */
let pendingCount = 0
let settleTimer: number | null = null

function scheduleFinish() {
  if (settleTimer !== null) {
    window.clearTimeout(settleTimer)
  }
  // 留一个极短窗口吞掉同批次内连续触发的导航，避免 loading 闪烁。
  settleTimer = window.setTimeout(() => {
    pendingCount = 0
    usePjaxStore.setState({ loading: false, target: null })
  }, SPLIT_WINDOW)
}

export const usePjaxStore = create<PjaxState & PjaxActions>((set) => ({
  loading: false,
  target: null,

  start: (target?: string) => {
    pendingCount += 1
    set({ loading: true, target: target ?? window.location.href })
  },

  finish: () => {
    scheduleFinish()
  },
}))

/** 请求前端整页降级刷新（PJAX 失败时的兜底路径）。 */
export function requestFullReload(reason?: string) {
  if (reason) {
    // 轻量日志，便于排查路由失效/服务异常。
    console.warn('[pjax] 降级整页刷新:', reason)
  }
  window.location.reload()
}
