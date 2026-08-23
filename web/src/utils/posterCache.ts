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

/** 会话内热点缓存：cacheKey -> objectURL */
const memUrls = new Map<string, string>()
/** 并发去重：同一 key 只发一次网络请求 */
const inflight = new Map<string, Promise<string>>()

let dbPromise: Promise<IDBDatabase> | null = null

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
        memUrls.set(key, objUrl)
        return objUrl
      }
    } catch {
      // IDB 打开/读取失败：继续走网络
    }

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
    memUrls.set(key, objUrl)
    return objUrl
  })()

  const settled = task.catch(() => url).finally(() => { inflight.delete(key) })
  inflight.set(key, settled)
  return settled
}
