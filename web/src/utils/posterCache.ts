/**
 * 应用级海报缓存（IndexedDB）。
 *
 * 为什么不用浏览器 HTTP 缓存 / Service Worker：
 * - 移动端浏览器对磁盘缓存的复用策略不可控，局域网 HTTP 环境下 SW 直接不可用；
 * - 海报 URL 里带 token，重新登录后 URL 变化会击穿所有以完整 URL 为键的缓存层。
 *
 * 设计：
 * - 缓存键 = 路径 + ?v= 版本戳（不含 token）：重新登录不失效，刮削 bump 版本后自动拉新；
 * - 内存层（objectURL Map）→ IndexedDB 层 → 网络，逐级回退；
 * - 占位 SVG（X-Poster-Placeholder）不落库，返回原始 URL 直接展示；
 * - IDB 不可用时整体退化为普通 <img> 直连，零功能损失。
 */

const DB_NAME = 'nowen-poster-cache'
const STORE = 'posters'
const DB_VERSION = 1

/**
 * 会话内热点缓存：cacheKey -> objectURL
 *
 * 关键点：objectURL 持有内存中 Blob 的强引用，且不会自动释放，必须显式
 * revoke 才能归还内存。若不设上限，持续翻页 / 浏览大媒体库会让这个 Map
 * 无限增长，最终把标签页内存耗尽导致整站卡死。这里设一个上限，按 FIFO
 * 淘汰并 revoke 最旧的 objectURL（被淘汰项在 IndexedDB 中仍有持久副本，
 * 再次滚动到时能秒级重建），保证内存有界。
 */
const MAX_MEM_URLS = 400
const memUrls = new Map<string, string>()
/** 并发去重：同一 key 只发一次网络请求 */
const inflight = new Map<string, Promise<string>>()

/**
 * 网络拉取并发上限：单页会同时渲染几十张海报，若全部瞬间发到后端，
 * 会击穿服务端的全局限流（429，600 请求/分钟/IP），导致海报与页面请求
 * 一起被拒（表现为强制刷新 ERR_INVALID_RESPONSE / 整站短暂不可访问）。
 * 这里用信号量把并发的网络海报请求控制在少量，未轮到的一律排队，
 * 既保证海报最终都能加载，又不会突发打爆后端限流。
 */
const MAX_CONCURRENT_FETCH = 3
let activeFetches = 0
const fetchWaiters: Array<() => void> = []

function acquireToken(): Promise<void> {
  if (activeFetches < MAX_CONCURRENT_FETCH) {
    activeFetches += 1
    return Promise.resolve()
  }
  return new Promise((resolve) => {
    fetchWaiters.push(() => {
      activeFetches += 1
      resolve()
    })
  })
}

function releaseToken() {
  activeFetches -= 1
  const next = fetchWaiters.shift()
  if (next) next()
}

let dbPromise: Promise<IDBDatabase> | null = null

function setMemUrl(key: string, objUrl: string) {
  // 已存在同一 key 的 objectURL，直接复用，避免重复创建与占用。
  if (memUrls.has(key)) return
  memUrls.set(key, objUrl)
  while (memUrls.size > MAX_MEM_URLS) {
    const oldestKey = memUrls.keys().next().value as string
    const oldestUrl = memUrls.get(oldestKey)
    memUrls.delete(oldestKey)
    try {
      if (oldestUrl) URL.revokeObjectURL(oldestUrl)
    } catch {
      // revoke 失败不影响其余缓存
    }
  }
}

function openDb(): Promise<IDBDatabase> {
  if (!dbPromise) {
    dbPromise = new Promise((resolve, reject) => {
      const req = indexedDB.open(DB_NAME, DB_VERSION)
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains(STORE)) {
          req.result.createObjectStore(STORE, { keyPath: 'key' })
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error)
    })
    dbPromise.catch(() => { dbPromise = null })
  }
  return dbPromise
}

/** 稳定缓存键：路径 + 版本戳，剔除 token 等易变参数 */
export function posterCacheKey(url: string): string {
  try {
    const u = new URL(url, window.location.origin)
    const version = u.searchParams.get('v') || ''
    return version ? `${u.pathname}@${version}` : u.pathname
  } catch {
    return url
  }
}

function idbGet(db: IDBDatabase, key: string): Promise<Blob | null> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).get(key)
    req.onsuccess = () => resolve(req.result?.blob instanceof Blob ? req.result.blob : null)
    req.onerror = () => reject(req.error)
  })
}

function idbPut(db: IDBDatabase, key: string, blob: Blob): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).put({ key, blob, ts: Date.now() })
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

/**
 * 同步快查：仅返回内存中已就绪的 objectURL；未命中返回 null。
 *
 * 用于渲染时「先画出来再升级」：海报已在会话内存缓存（同页翻回、快速来回切页）
 * 时，同步返回 objectURL，<img> 无需等异步解析即可立刻显示，避免快速分页时
 * 海报长时间停留在占位态（加载不完全）。
 */
export function getCachedPosterUrlSync(url: string): string | null {
  if (!url || /^(blob:|data:)/i.test(url)) return null
  return memUrls.get(posterCacheKey(url)) ?? null
}

/**
 * 把海报 URL 解析为可立即展示的地址：
 * 命中缓存时返回本地 objectURL（秒出、零网络）；未命中时网络拉取并落库。
 * 任何异常都返回原始 URL（等价于未启用缓存的普通 <img> 行为）。
 */
export function resolvePosterUrl(url: string): Promise<string> {
  if (!url || /^(blob:|data:)/i.test(url)) return Promise.resolve(url)
  try {
    if (new URL(url, window.location.origin).origin !== window.location.origin) return Promise.resolve(url)
  } catch {
    return Promise.resolve(url)
  }
  if (!('indexedDB' in window)) return Promise.resolve(url)

  const key = posterCacheKey(url)
  const mem = memUrls.get(key)
  if (mem) return Promise.resolve(mem)
  const pending = inflight.get(key)
  if (pending) return pending

  const task = (async (): Promise<string> => {
    try {
      const db = await openDb()
      const blob = await idbGet(db, key)
      if (blob) {
        const objUrl = URL.createObjectURL(blob)
        setMemUrl(key, objUrl)
        return objUrl
      }
    } catch {
      // IDB 打开/读取失败：继续走网络
    }

    await acquireToken()
    try {
      const resp = await fetch(url)
      if (!resp.ok) return url
      // 占位图（媒体暂无海报）：不缓存，直接展示服务端返回的 SVG
      if (resp.headers.get('X-Poster-Placeholder')) return url

      const blob = await resp.blob()
      if (!blob.type.startsWith('image/') || blob.size === 0) return url

      try {
        const db = await openDb()
        await idbPut(db, key, blob)
      } catch {
        // 写库失败不影响本次展示
      }
      const objUrl = URL.createObjectURL(blob)
      setMemUrl(key, objUrl)
      return objUrl
    } finally {
      releaseToken()
    }
  })()

  const settled = task.catch(() => url).finally(() => { inflight.delete(key) })
  inflight.set(key, settled)
  return settled
}
