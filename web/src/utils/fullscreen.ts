/**
 * 跨浏览器全屏工具
 *
 * 移动端（尤其 iOS Safari / 各种国产浏览器与 WebView）对 Fullscreen API 的支持残缺：
 *   - 标准 API: requestFullscreen / exitFullscreen / fullscreenElement
 *   - WebKit 前缀: webkitRequestFullscreen / webkitExitFullscreen / webkitFullscreenElement
 *   - Firefox 前缀: mozRequestFullScreen / mozCancelFullScreen / mozFullScreenElement
 *   - IE/旧 Edge 前缀: msRequestFullscreen / msExitFullscreen / msFullscreenElement
 *   - iOS Safari (iPhone): 元素级全屏完全不可用，只能用 <video> 的原生全屏
 *     webkitEnterFullscreen() / webkitExitFullscreen()
 *
 * 全部失败时由调用方（usePlayerFullscreen）降级为 CSS 伪全屏。
 */

export const FAKE_FULLSCREEN_CLASS = 'player-fake-fullscreen'

type PrefixedRequestElement = HTMLElement & {
  webkitRequestFullscreen?: () => Promise<void> | void
  mozRequestFullScreen?: () => Promise<void> | void
  msRequestFullscreen?: () => Promise<void> | void
}

type PrefixedDocument = Document & {
  webkitFullscreenElement?: Element | null
  mozFullScreenElement?: Element | null
  msFullscreenElement?: Element | null
  webkitExitFullscreen?: () => Promise<void> | void
  mozCancelFullScreen?: () => Promise<void> | void
  msExitFullscreen?: () => Promise<void> | void
}

export type NativeFullscreenVideoElement = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void
  webkitExitFullscreen?: () => void
}

/** 当前处于全屏的元素（兼容各前缀），无则返回 null */
export function getFullscreenElement(): Element | null {
  const doc = document as PrefixedDocument
  return doc.fullscreenElement
    || doc.webkitFullscreenElement
    || doc.mozFullScreenElement
    || doc.msFullscreenElement
    || null
}

/** 元素是否支持进入元素级全屏（含前缀实现） */
export function supportsElementFullscreen(element: HTMLElement): boolean {
  const fsEl = element as PrefixedRequestElement
  return typeof element.requestFullscreen === 'function'
    || typeof fsEl.webkitRequestFullscreen === 'function'
    || typeof fsEl.mozRequestFullScreen === 'function'
    || typeof fsEl.msRequestFullscreen === 'function'
}

/** 进入元素级全屏，自动尝试各前缀；全部失败返回 false */
export async function enterElementFullscreen(element: HTMLElement): Promise<boolean> {
  const fsEl = element as PrefixedRequestElement
  const candidates = [
    element.requestFullscreen?.bind(element),
    fsEl.webkitRequestFullscreen?.bind(fsEl),
    fsEl.mozRequestFullScreen?.bind(fsEl),
    fsEl.msRequestFullscreen?.bind(fsEl),
  ]
  for (const request of candidates) {
    if (!request) continue
    try {
      await request()
      return true
    } catch {
      // 某些浏览器标准 API 存在但会 reject（如无 allowfullscreen 的 iframe），继续尝试前缀
    }
  }
  return false
}

/** 退出元素级全屏，自动尝试各前缀；全部失败返回 false */
export async function exitElementFullscreen(): Promise<boolean> {
  if (!getFullscreenElement()) return false
  const doc = document as PrefixedDocument
  const candidates = [
    document.exitFullscreen?.bind(document),
    doc.webkitExitFullscreen?.bind(doc),
    doc.mozCancelFullScreen?.bind(doc),
    doc.msExitFullscreen?.bind(doc),
  ]
  for (const exit of candidates) {
    if (!exit) continue
    try {
      await exit()
      return true
    } catch { /* 尝试下一个 */ }
  }
  return false
}

/** iOS 原生视频全屏是否可用（iPhone Safari 上唯一有效的全屏方式） */
export function supportsNativeVideoFullscreen(video: HTMLVideoElement | null): boolean {
  return !!video && typeof (video as NativeFullscreenVideoElement).webkitEnterFullscreen === 'function'
}

/** 进入 iOS 原生视频全屏 */
export function enterNativeVideoFullscreen(video: HTMLVideoElement | null): boolean {
  const nativeVideo = video as NativeFullscreenVideoElement | null
  if (!nativeVideo || typeof nativeVideo.webkitEnterFullscreen !== 'function') return false
  try {
    nativeVideo.webkitEnterFullscreen()
    return true
  } catch {
    return false
  }
}

/** 退出 iOS 原生视频全屏 */
export function exitNativeVideoFullscreen(video: HTMLVideoElement | null): void {
  const nativeVideo = video as NativeFullscreenVideoElement | null
  if (typeof nativeVideo?.webkitExitFullscreen === 'function') {
    try {
      nativeVideo.webkitExitFullscreen()
    } catch { /* ignore */ }
  }
}

/** 是否为移动设备（含 iPadOS 13+ 伪装成 Mac 的情况） */
export function isMobileDevice(): boolean {
  if (typeof navigator === 'undefined') return false
  const ua = navigator.userAgent || ''
  const iPadOS = /Macintosh/i.test(ua) && navigator.maxTouchPoints > 1
  return /Android|iPhone|iPad|iPod|Mobile|Silk|Kindle/i.test(ua) || iPadOS
}

/**
 * 移动端进入全屏后锁定横屏。
 * 仅 Android Chrome 等支持 screen.orientation.lock；iOS 会静默失败（原生全屏自带旋转按钮）。
 * 注意：旧版 TS lib.dom 未声明 lock/unlock，这里用结构化类型自行声明以保证编译通过。
 */
interface LockableScreenOrientation {
  lock?: (orientation: string) => Promise<void>
  unlock?: () => void
}

function getLockableOrientation(): LockableScreenOrientation | undefined {
  const orientation: unknown = typeof screen !== 'undefined' ? screen.orientation : undefined
  return orientation as LockableScreenOrientation | undefined
}

export async function lockOrientationLandscape(): Promise<void> {
  try {
    await getLockableOrientation()?.lock?.('landscape')
  } catch { /* 不支持或被拒绝时静默忽略 */ }
}

/** 解除横屏锁定 */
export function unlockOrientation(): void {
  try {
    getLockableOrientation()?.unlock?.()
  } catch { /* ignore */ }
}
