import { useEffect, useRef } from 'react'
import { runHeroParticleFx } from '@/utils/heroParticleFx'

interface HeroParticleTransitionProps {
  className?: string
  sourceSrc?: string | null
  direction?: 1 | -1
  onDone?: () => void
}

export default function HeroParticleTransition({
  className,
  sourceSrc,
  direction = 1,
  onDone,
}: HeroParticleTransitionProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const onDoneRef = useRef(onDone)
  onDoneRef.current = onDone

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    return runHeroParticleFx(canvas, {
      sourceSrc,
      direction,
      onDone: () => onDoneRef.current?.(),
    })
  }, [sourceSrc, direction])

  return (
    <div className={className} aria-hidden="true">
      <canvas ref={canvasRef} className="h-full w-full" />
    </div>
  )
}
