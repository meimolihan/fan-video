import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { usePjaxStore, requestFullReload } from '@/stores/pjax'

/**
 * PJAX 协调桥梁：在 React SPA 之上补齐“局部刷新”语义，不重写任何业务组件。
 *
 * 职责：
 *  - 劫持站内 <a> 链接（document 捕获阶段委托），按规则过滤外部链接、
 *    _blank / download、修饰键(ctrl/meta/shift/alt)与右键等，仅对合法的
 *    同源 GET 导航触发全局 loading 与 X-PJAX 预取。
 *  - X-PJAX 预取：向后端带 X-PJAX 头请求目标，用于及早发现服务异常
 *    （>=400 或网络失败）并降级整页刷新；成功后交由 react-router 完成片段切换。
 *  - popstate / 前进后退由 react-router 原生承担；这里仅在 location 变化后
 *    结束 loading，保证指示状态始终收敛。
 *
 * 刻意不做 DOM 级 innerHTML 替换：React 虚拟 DOM 由 react-router 管理，
 * 片段切换 / 保留侧栏 / 懒加载 / 播放器均由现有实现完成，这里只做协调。
 */
export default function PjaxBridge() {
  const location = useLocation()
  const current = location.pathname + location.search
  const prevRef = useRef(current)

  // location 变化 → 路由过渡已完成（PUSH/POP 均由 router 触发），结束 loading。
  useEffect(() => {
    const prev = prevRef.current
    prevRef.current = current
    if (prev !== current) {
      usePjaxStore.getState().finish()
    }
  }, [current])

  // 安装全局链接委托的捕获监听：只观察 + 触发 loading/预取，不 preventDefault，
  // 以免与 react-router <Link> 自己的点击处理冲突。
  useEffect(() => {
    function isPjaxable(link: HTMLAnchorElement): string | null {
      if (!link.href) return null

      // 非 GET 语义：download / _blank 等新开页不在局部刷新范围。
      if (link.hasAttribute('download')) return null
      const target = link.getAttribute('target')
      if (target && target.trim() !== '' && target.trim().toLowerCase() !== '_self') return null

      let url: URL
      try {
        url = new URL(link.href, window.location.origin)
      } catch {
        return null
      }

      // 仅处理 http(s) 同源链接。
      if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
      if (url.origin !== window.location.origin) return null

      // 纯锚点(#/hash)或指向当前地址不产生页面过渡。
      if (url.pathname === window.location.pathname && url.search === window.location.search) return null

      return url.pathname + url.search
    }

    function onDocumentClick(event: MouseEvent) {
      // 修饰键/非左键：属于用户“新开/新标签”意图，不劫持。
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return

      const element = event.target instanceof Element ? (event.target as Element).closest('a[href]') : null
      if (!element || !(element instanceof HTMLAnchorElement)) return
      if (element.getAttribute('aria-disabled') === 'true') return

      const target = isPjaxable(element)
      if (!target) return

      usePjaxStore.getState().start(target)
      void probeServer()
    }

    let aborted = false

    async function probeServer() {
      let response: Response
      try {
        response = await fetch(window.location.pathname + window.location.search, {
          headers: { 'X-PJAX': 'true' },
        })
      } catch {
        if (!aborted) requestFullReload('X-PJAX 预取网络失败')
        return
      }
      if (aborted) return
      if (!response.ok) {
        // 服务端异常：PJAX 片段无法正常交付，降级整页刷新。
        requestFullReload(`服务端返回 ${response.status} 无法交付页面`)
      }
    }

    document.addEventListener('click', onDocumentClick, true)
    return () => {
      aborted = true
      document.removeEventListener('click', onDocumentClick, true)
    }
  }, [])

  return null
}
