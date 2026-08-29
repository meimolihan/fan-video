import { ArrowUp } from 'lucide-react'
import { useEffect, useState } from 'react'

export interface BackToTopProps {
  /** Scroll container element id. Defaults to the main scroll container. */
  containerId?: string
  /** Scroll distance (px) after which the button becomes visible. */
  threshold?: number
}

export default function BackToTop({
  containerId = 'main-scroll-container',
  threshold = 320,
}: BackToTopProps) {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const el = document.getElementById(containerId)
    if (!el) return

    let ticking = false
    const update = () => {
      ticking = false
      setVisible(el.scrollTop > threshold)
    }
    const onScroll = () => {
      if (ticking) return
      ticking = true
      requestAnimationFrame(update)
    }
    update()
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      el.removeEventListener('scroll', onScroll)
    }
  }, [containerId, threshold])

  const handleClick = () => {
    const el = document.getElementById(containerId)
    if (!el) return
    el.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <button
      type="button"
      className="nv-back-to-top"
      aria-label="返回顶部"
      title="返回顶部"
      hidden={!visible}
      onClick={handleClick}
    >
      <ArrowUp size={18} aria-hidden="true" />
    </button>
  )
}
