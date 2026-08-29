import { useEffect, useRef } from 'react'

const TAU = Math.PI * 2
const DURATION = 980
const MAX_W = 1280
const DPR_CAP = 1.5
const TARGET_SHATTER = 1900

interface HeroParticleTransitionProps {
  className?: string
  sourceSrc?: string | null
  direction?: 1 | -1
  onDone?: () => void
}

interface Sample { x: number; y: number; r: number; g: number; b: number }

interface Particle {
  x: number
  y: number
  vx: number
  vy: number
  r: number
  delay: number
  life: number
  color: string
  kind: 0 | 1 | 2
}

function rgb(r: number, g: number, b: number, a: number) {
  return `rgba(${r | 0},${g | 0},${b | 0},${a.toFixed(3)})`
}

function hexToRgb(hex: string) {
  const value = hex.replace('#', '')
  if (value.length < 6) return { r: 130, g: 100, b: 250 }
  return {
    r: parseInt(value.slice(0, 2), 16),
    g: parseInt(value.slice(2, 4), 16),
    b: parseInt(value.slice(4, 6), 16),
  }
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
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    const canvasCtx = ctx

    const rect = canvas.getBoundingClientRect()
    const cssW = Math.max(1, rect.width)
    const cssH = Math.max(1, rect.height)
    const dpr = Math.min(window.devicePixelRatio || 1, DPR_CAP)
    const scale = Math.min(1, MAX_W / cssW)
    const W = Math.round(cssW * scale)
    const H = Math.round(cssH * scale)
    canvas.width = Math.round(W * dpr)
    canvas.height = Math.round(H * dpr)
    canvasCtx.setTransform((cssW * dpr) / W, 0, 0, (cssW * dpr) / W, 0, 0)

    const cx = W / 2
    const cy = H / 2
    const start = performance.now()
    let raf = 0
    let particles: Particle[] = []
    let built = false

    const accent = (() => {
      try {
        const value = getComputedStyle(document.documentElement).getPropertyValue('--nv-action-primary').trim()
        return value ? hexToRgb(value) : { r: 130, g: 100, b: 250 }
      } catch {
        return { r: 130, g: 100, b: 250 }
      }
    })()

    function build(samples: Sample[]) {
      const list: Particle[] = []

      for (let i = 0; i < samples.length; i += 1) {
        const s = samples[i]
        const dx = s.x - cx
        const dy = s.y - cy
        const len = Math.max(1, Math.hypot(dx, dy))
        const nx = dx / len
        const ny = dy / len
        const speed = 130 + Math.random() * 260
        list.push({
          x: s.x,
          y: s.y,
          vx: nx * speed * (0.6 + Math.random() * 0.8) + direction * 26 + (Math.random() - 0.5) * 36,
          vy: ny * speed * (0.55 + Math.random() * 0.7) - 16 + (Math.random() - 0.5) * 56,
          r: 1.1 + Math.random() * 2.4,
          delay: Math.pow(Math.random(), 1.6) * 300,
          life: 0.5 + Math.random() * 0.42,
          color: rgb(s.r, s.g, s.b, 1),
          kind: 0,
        })
      }

      for (let i = 0; i < 64; i += 1) {
        const side = Math.floor(Math.random() * 4)
        const along = Math.random()
        const depth = 6 + Math.random() * 18
        const warm = Math.random() < 0.4
        const out = 90 + Math.random() * 230
        let x = 0
        let y = 0
        let vx = 0
        let vy = 0
        if (side === 0) { x = depth; y = along * H; vx = out }
        else if (side === 1) { x = W - depth; y = along * H; vx = -out }
        else if (side === 2) { x = along * W; y = depth; vy = out }
        else { x = along * W; y = H - depth; vy = -out }
        list.push({
          x,
          y,
          vx: vx * (0.7 + Math.random() * 0.6) + direction * 30,
          vy: vy * (0.7 + Math.random() * 0.6) + (Math.random() - 0.5) * 60,
          r: 1 + Math.random() * 2,
          delay: Math.random() * 90,
          life: 0.34 + Math.random() * 0.4,
          color: warm ? rgb(255, 216, 168, 1) : rgb(255, 255, 255, 1),
          kind: 1,
        })
      }

      for (let i = 0; i < 46; i += 1) {
        const from = Math.floor(Math.random() * 4)
        const along = Math.random()
        const tx = W * (0.18 + Math.random() * 0.64)
        const ty = H * (0.26 + Math.random() * 0.48)
        let sx = 0
        let sy = 0
        if (from === 0) { sx = -10; sy = along * H }
        else if (from === 1) { sx = W + 10; sy = along * H }
        else if (from === 2) { sx = along * W; sy = -10 }
        else { sx = along * W; sy = H + 10 }
        const dx = tx - sx
        const dy = ty - sy
        const len = Math.max(1, Math.hypot(dx, dy))
        const spd = 260 + Math.random() * 260
        list.push({
          x: sx,
          y: sy,
          vx: (dx / len) * spd,
          vy: (dy / len) * spd,
          r: 1.2 + Math.random() * 2,
          delay: 90 + Math.random() * 240,
          life: 0.42 + Math.random() * 0.34,
          color: Math.random() < 0.5 ? rgb(255, 255, 255, 1) : rgb(accent.r, accent.g, accent.b, 1),
          kind: 2,
        })
      }

      particles = list
      built = true
    }

    function buildFallback() {
      const samples: Sample[] = []
      const gap = Math.max(7, Math.round(Math.sqrt((W * H) / TARGET_SHATTER)))
      for (let y = 0; y < H; y += gap) {
        for (let x = 0; x < W; x += gap) {
          const pick = Math.random()
          const c = pick < 0.45
            ? { r: 255, g: 255, b: 255 }
            : pick < 0.7
              ? { r: accent.r, g: accent.g, b: accent.b }
              : { r: 255, g: 209, b: 161 }
          samples.push({ x: x + Math.random() * gap, y: y + Math.random() * gap, ...c })
        }
      }
      build(samples)
    }

    function trySample() {
      if (!sourceSrc) {
        buildFallback()
        return
      }
      const img = new Image()
      img.crossOrigin = 'anonymous'
      img.onload = () => {
        const iw = img.naturalWidth
        const ih = img.naturalHeight
        if (!iw || !ih) {
          buildFallback()
          return
        }
        const cover = Math.max(W / iw, H / ih)
        const dw = iw * cover
        const dh = ih * cover
        const off = document.createElement('canvas')
        off.width = W
        off.height = H
        const octx = off.getContext('2d', { willReadFrequently: true })
        if (!octx) {
          buildFallback()
          return
        }
        octx.drawImage(img, (W - dw) / 2, (H - dh) / 2, dw, dh)
        const imageData = octx.getImageData(0, 0, W, H)
        const pixels = imageData.data
        const gap = Math.max(6, Math.round(Math.sqrt((W * H) / TARGET_SHATTER)))
        const samples: Sample[] = []
        for (let y = 0; y < H; y += gap) {
          for (let x = 0; x < W; x += gap) {
            const idx = (y * W + x) * 4
            if (pixels[idx + 3] < 40) continue
            const r = pixels[idx]
            const g = pixels[idx + 1]
            const b = pixels[idx + 2]
            const lum = r * 0.299 + g * 0.587 + b * 0.114
            if (lum < 12 && Math.random() > 0.2) continue
            samples.push({ x: x + Math.random() * gap, y: y + Math.random() * gap, r, g, b })
          }
        }
        if (samples.length > 24) build(samples)
        else buildFallback()
      }
      img.onerror = () => buildFallback()
      img.src = sourceSrc
    }

    trySample()

    function framePass() {
      const elapsed = performance.now() - start

      if (!built) {
        if (elapsed > 1400) buildFallback()
        raf = requestAnimationFrame(framePass)
        return
      }

      canvasCtx.clearRect(0, 0, W, H)
      if (elapsed < DURATION + 80) {
        canvasCtx.globalCompositeOperation = 'lighter'
        const sec = elapsed / 1000
        const endFade = Math.max(0, (elapsed - (DURATION - 170)) / 170)
        for (let i = 0; i < particles.length; i += 1) {
          const p = particles[i]
          const age = sec - p.delay / 1000
          if (age <= 0 || age > p.life) continue
          const k = age / p.life
          const fadeIn = age < 0.05 ? age / 0.05 : 1
          const fade = fadeIn * (1 - k * k) * (1 - endFade)
          if (fade <= 0.01) continue
          const drag = 1 - k * 0.35
          const x = p.x + p.vx * age * drag
          const y = p.y + p.vy * age * drag + (p.kind === 0 ? 24 * age * age : 0)
          const r = p.r * (0.5 + 0.5 * (1 - k))
          canvasCtx.globalAlpha = fade * (p.kind === 1 ? 0.95 : 0.9)
          canvasCtx.fillStyle = p.color
          canvasCtx.beginPath()
          canvasCtx.arc(x, y, r, 0, TAU)
          canvasCtx.fill()
        }
        canvasCtx.globalAlpha = 1
        canvasCtx.globalCompositeOperation = 'source-over'
        raf = requestAnimationFrame(framePass)
        return
      }
      onDoneRef.current?.()
    }

    raf = requestAnimationFrame(framePass)
    return () => {
      cancelAnimationFrame(raf)
    }
  }, [sourceSrc, direction])

  return (
    <div className={className} aria-hidden="true">
      <canvas ref={canvasRef} className="h-full w-full" />
    </div>
  )
}