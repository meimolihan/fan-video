import { useCallback } from 'react'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import zhCN from './locales/zh-CN'

// 支持的语言列表
export const SUPPORTED_LOCALES = [
  { code: 'zh-CN', name: '简体中文', flag: '🇨🇳' },
  { code: 'en-US', name: 'English', flag: '🇺🇸' },
  { code: 'ja-JP', name: '日本語', flag: '🇯🇵' },
] as const

export type LocaleCode = typeof SUPPORTED_LOCALES[number]['code']

// zh-CN 是默认语言，内联进主 bundle，保证首屏零异步依赖；
// en-US / ja-JP 通过动态 import 切成独立 chunk，按需加载（省去首屏解析 ~70KB 语言包）。
const bundledMessages = zhCN
const localeMessages: Record<string, Record<string, string> | undefined> = {
  'zh-CN': bundledMessages,
}

// 异步加载语言包，加载完成后缓存。失败时回退到 zh（t 天然兜底），不阻断 UI。
async function loadLocaleMessages(code: LocaleCode): Promise<Record<string, string>> {
  if (code === 'zh-CN') return bundledMessages
  const cached = localeMessages[code]
  if (cached) return cached
  let mod: { default: Record<string, string> }
  switch (code) {
    case 'en-US':
      mod = await import('./locales/en-US')
      break
    case 'ja-JP':
      mod = await import('./locales/ja-JP')
      break
    default:
      return bundledMessages
  }
  localeMessages[code] = mod.default
  return mod.default
}

// i18n Store
interface I18nStore {
  locale: LocaleCode
  /** 语言包装载完成后自增，驱动 useTranslation 侧响应式重渲染 */
  nonce: number
  setLocale: (locale: LocaleCode) => void
}

// 检测浏览器语言
function detectBrowserLocale(): LocaleCode {
  const legacy = navigator as Navigator & { userLanguage?: string }
  const browserLang = navigator.language || legacy.userLanguage || ''
  if (browserLang.startsWith('zh')) return 'zh-CN'
  if (browserLang.startsWith('ja')) return 'ja-JP'
  if (browserLang.startsWith('en')) return 'en-US'
  return 'zh-CN' // 默认中文
}

export const useI18nStore = create<I18nStore>()(
  persist(
    (set) => ({
      locale: detectBrowserLocale(),
      nonce: 0,
      setLocale: (locale: LocaleCode) => {
        set({ locale })
        // 更新 HTML lang 属性
        document.documentElement.lang = locale
        if (locale !== 'zh-CN') {
          loadLocaleMessages(locale)
            .catch(() => {})
            .finally(() => {
              // 语言包就绪后 bump nonce，让正在渲染使用该语言的组件立即生效
              set((s) => (s.locale === locale ? { nonce: s.nonce + 1 } : {}))
            })
        }
      },
    }),
    {
      name: 'nowen-i18n',
      // 只持久化 locale；nonce 为运行时信号，无需落盘
      partialize: (s) => ({ locale: s.locale }),
    }
  )
)

// 共享的插值逻辑：把 {count} 替换为参数
function interpolate(text: string, params?: Record<string, string | number>): string {
  if (!params) return text
  Object.entries(params).forEach(([k, v]) => {
    text = text.replace(new RegExp(`\\{${k}\\}`, 'g'), String(v))
  })
  return text
}

// 取当前语言消息；未装载（异步加载中）时退化为 zh 兜底
function messagesFor(code: LocaleCode): Record<string, string> {
  return localeMessages[code] || bundledMessages
}

/**
 * 翻译函数（同步）。en/ja 语言包尚未加载完成时返回 zh 对应文案，加载完成后即切换。
 * @param key 翻译键
 * @param params 插值参数，如 { count: 5 }
 * @returns 翻译后的文本
 */
export function t(key: string, params?: Record<string, string | number>): string {
  const locale = useI18nStore.getState().locale
  const text = messagesFor(locale)[key] || bundledMessages[key] || key
  return interpolate(text, params)
}

/**
 * React Hook: 获取翻译函数（响应式）
 * 当语言切换、且对应语言包装载完成时，使用此 hook 的组件会自动重新渲染
 */
export function useTranslation() {
  const locale = useI18nStore((s) => s.locale)
  const nonce = useI18nStore((s) => s.nonce)
  const setLocale = useI18nStore((s) => s.setLocale)

  const translate = useCallback(
    (key: string, params?: Record<string, string | number>): string => {
      const text = messagesFor(locale)[key] || bundledMessages[key] || key
      return interpolate(text, params)
    },
    [locale, nonce]
  )

  return { t: translate, locale, setLocale }
}

// 初始化 i18n（App 启动时调用）。若持久化的语言不是默认 zh，则静默预载对应语言包，
// 首屏先用 zh 兜底渲染，语言包就绪后自动切换为目标语言。
export function initI18n() {
  const state = useI18nStore.getState()
  document.documentElement.lang = state.locale
  if (state.locale !== 'zh-CN') {
    loadLocaleMessages(state.locale)
      .catch(() => {})
      .finally(() => {
        useI18nStore.setState((s) => (s.locale === state.locale ? { nonce: s.nonce + 1 } : {}))
      })
  }
}