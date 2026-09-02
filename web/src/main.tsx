import React from 'react'
import ReactDOM from 'react-dom/client'
import { LazyMotion, domAnimation } from 'framer-motion'
import App from './App'
import { initTheme } from './stores/theme'
import { initI18n } from './i18n'
import { installSubtitleTrackActivationGuard } from './utils/subtitleTrackActivation'
import { installMediaDetailHeroEnhancer } from './utils/mediaDetailHeroEnhancer'
import './styles/base.css'
import './styles/pages-theme.css'
import './styles/app-ui.css'

const SW_DEV_RELOAD_KEY = 'nowen-sw-dev-cleanup-reload'
const SW_UPDATE_RELOAD_KEY = 'nowen-sw-production-update-reload'

function dismissSplash() {
  const splash = document.getElementById('splash')
  if (!splash || splash.classList.contains('hide')) return
  // 至少展示 600ms，避免资源秒开时开屏闪烁
  const delay = Math.max(0, 600 - performance.now())
  window.setTimeout(() => {
    splash.classList.add('hide')
    window.setTimeout(() => splash.remove(), 450)
  }, delay)
}

async function cleanupDevelopmentServiceWorker() {
  try {
    const registrations = await navigator.serviceWorker.getRegistrations()
    await Promise.all(
      registrations
        .filter((registration) => new URL(registration.scope).origin === window.location.origin)
        .map((registration) => registration.unregister()),
    )

    if ('caches' in window) {
      const keys = await window.caches.keys()
      await Promise.all(keys.filter((key) => key.startsWith('nowen-')).map((key) => window.caches.delete(key)))
    }

    if (navigator.serviceWorker.controller && sessionStorage.getItem(SW_DEV_RELOAD_KEY) !== '1') {
      sessionStorage.setItem(SW_DEV_RELOAD_KEY, '1')
      window.location.reload()
      return
    }

    sessionStorage.removeItem(SW_DEV_RELOAD_KEY)
  } catch (error) {
    console.warn('[PWA] Failed to cleanup development service worker state:', error)
  }
}

async function registerProductionServiceWorker() {
  try {
    const registration = await navigator.serviceWorker.register('/assets/sw.js', { scope: '/' })

    const activateUpdate = (worker: ServiceWorker | null) => {
      if (!worker || worker.state !== 'installed' || !navigator.serviceWorker.controller) return
      worker.postMessage({ type: 'SKIP_WAITING' })
    }

    registration.addEventListener('updatefound', () => {
      const installing = registration.installing
      installing?.addEventListener('statechange', () => activateUpdate(installing))
    })

    if (registration.waiting && navigator.serviceWorker.controller) {
      registration.waiting.postMessage({ type: 'SKIP_WAITING' })
    }

    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (sessionStorage.getItem(SW_UPDATE_RELOAD_KEY) === '1') return
      sessionStorage.setItem(SW_UPDATE_RELOAD_KEY, '1')
      window.location.reload()
    })

    window.addEventListener('load', () => {
      sessionStorage.removeItem(SW_UPDATE_RELOAD_KEY)
      void registration.update()
    }, { once: true })
  } catch (error) {
    console.warn('[PWA] Failed to register service worker:', error)
  }
}

if ('serviceWorker' in navigator) {
  if (import.meta.env.DEV) void cleanupDevelopmentServiceWorker()
  else void registerProductionServiceWorker()
}

installSubtitleTrackActivationGuard()
installMediaDetailHeroEnhancer()

// 兜底：无论下方渲染是否同步抛错，开屏动画都必须在短时间内移除，
// 否则任何启动期错误都会把用户永久挡在开屏画面上。
window.setTimeout(dismissSplash, 1500)

try {
  initTheme()
  initI18n()
} catch (error) {
  console.error('[boot] init failed:', error)
}

try {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <LazyMotion features={domAnimation} strict>
        <App />
      </LazyMotion>
    </React.StrictMode>,
  )
} catch (error) {
  console.error('[boot] React 挂载失败:', error)
}

requestAnimationFrame(dismissSplash)
