import type { HTMLAttributes } from 'react'
import clsx from 'clsx'

export type MediaGridVariant = 'standard' | 'poster'

export interface MediaGridProps extends HTMLAttributes<HTMLDivElement> {
  variant?: MediaGridVariant
}

export function MediaGrid({ variant = 'standard', className, children, ...props }: MediaGridProps) {
  return (
    <div
      {...props}
      className={clsx(
        'nv-ui-media-grid',
        variant === 'standard'
          ? 'nv-media-grid'
          : 'nv-browse-poster-grid grid gap-x-2.5 gap-y-4',
        className,
      )}
      data-variant={variant}
    >
      {children}
    </div>
  )
}

export default MediaGrid
