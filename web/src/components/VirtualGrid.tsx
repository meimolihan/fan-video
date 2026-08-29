import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import clsx from 'clsx'

interface VirtualGridProps {
  count: number
  children: (index: number) => ReactNode
  minItemWidth?: number
  gapX?: number
  gapY?: number
  rowHeight?: number
  textHeight?: number
  className?: string
  overscan?: number
  threshold?: number
  'aria-label'?: string
}

const DEFAULT_MIN_ITEM_WIDTH = 150

function findScrollContainer(el: HTMLElement | null): HTMLElement | null {
  let node: HTMLElement | null = el
  while (node) {
    const style = getComputedStyle(node)
    if (node.id === 'main-scroll-container' || style.overflowY === 'auto' || style.overflowY === 'scroll') {
      return node
    }
    node = node.parentElement
  }
  return null
}

export default function VirtualGrid({
  count,
  children,
  minItemWidth = DEFAULT_MIN_ITEM_WIDTH,
  gapX,
  gapY = 16,
  rowHeight,
  textHeight = 58,
  className,
  overscan = 4,
  threshold = 80,
  'aria-label': ariaLabel,
}: VirtualGridProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [measure, setMeasure] = useState<{ columns: number; gapX: number; gapY: number } | null>(null)
  const [gridScrollTop, setGridScrollTop] = useState(0)

  useLayoutEffect(() => {
    const el = containerRef.current
    if (!el || count <= threshold) return
    const rootStyle = getComputedStyle(el)
    const gapXFromCss = parseFloat(rootStyle.getPropertyValue('--nv-grid-gap-x')) || 14
    const gapYFromCss = parseFloat(rootStyle.getPropertyValue('--nv-grid-gap-y')) || 16
    const magicGapX = gapX ?? gapXFromCss
    const magicGapY = gapY ?? gapYFromCss

    const updateColumns = () => {
      const width = el.clientWidth
      if (!width) return
      const columns = Math.max(1, Math.floor((width + magicGapX) / (minItemWidth + magicGapX)))
      setMeasure((current) => {
        if (current && current.columns === columns) return current
        return { columns, gapX: magicGapX, gapY: magicGapY }
      })
    }
    updateColumns()
    const ro = new ResizeObserver(updateColumns)
    ro.observe(el)

    const scroller = findScrollContainer(el)
    if (!scroller) return () => ro.disconnect()

    const updateScroll = () => {
      const elTopInScroller = scroller.scrollTop + (el.getBoundingClientRect().top - scroller.getBoundingClientRect().top)
      setGridScrollTop(Math.max(0, scroller.scrollTop - elTopInScroller))
    }
    updateScroll()
    scroller.addEventListener('scroll', updateScroll, { passive: true })
    window.addEventListener('resize', updateScroll)
    window.addEventListener('scroll', updateScroll, true)
    return () => {
      ro.disconnect()
      scroller.removeEventListener('scroll', updateScroll)
      window.removeEventListener('resize', updateScroll)
      window.removeEventListener('scroll', updateScroll, true)
    }
  }, [count, minItemWidth, gapX, gapY, threshold])

  const itemWidth = measure && containerRef.current
    ? (containerRef.current.clientWidth - measure.gapX * (measure.columns - 1)) / measure.columns
    : minItemWidth
  const itemHeight = rowHeight ?? itemWidth * (2 / 3) + textHeight

  if (count <= threshold || !measure) {
    if (count === 0) return null
    return (
      <div ref={containerRef} className={clsx('nv-media-grid', className)} aria-label={ariaLabel}>
        {Array.from({ length: count }, (_, i) => children(i))}
      </div>
    )
  }

  const { columns, gapX: gx, gapY: gy } = measure
  const totalRows = Math.ceil(count / columns)
  const totalHeight = totalRows * itemHeight + (totalRows - 1) * gy
  const viewportHeight = containerRef.current?.parentElement
    ? (findScrollContainer(containerRef.current)?.clientHeight || window.innerHeight)
    : window.innerHeight

  const startRow = Math.max(0, Math.floor(gridScrollTop / (itemHeight + gy)) - overscan)
  const endRow = Math.min(totalRows, startRow + Math.ceil(viewportHeight / (itemHeight + gy)) + overscan * 2)

  const cells: ReactNode[] = []
  for (let row = startRow; row < endRow; row += 1) {
    const colOffset = row * columns
    for (let col = 0; col < columns; col += 1) {
      const index = colOffset + col
      if (index >= count) break
      cells.push(
        <div
          key={index}
          className="absolute left-0 top-0"
          style={{
            width: itemWidth,
            height: itemHeight,
            transform: `translate(${col * (itemWidth + gx)}px, ${row * (itemHeight + gy)}px)`,
          }}
        >
          {children(index)}
        </div>,
      )
    }
  }

  return (
    <div
      ref={containerRef}
      className={clsx('relative w-full', className)}
      style={{ height: totalHeight }}
      aria-label={ariaLabel}
      data-virtualized="true"
    >
      {cells}
    </div>
  )
}
