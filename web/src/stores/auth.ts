import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import type { User } from '@/types'

interface AuthState {
  token: string | null
  user: User | null
  isAuthenticated: boolean

  setAuth: (token: string, user: User) => void
  logout: () => void
  updateUser: (user: User) => void
}

// 安全存储策略：
// - 登录令牌等敏感信息只写入 sessionStorage（关闭标签页即失效），
//   避免长期留存在 localStorage 上扩大 XSS 窃取放大面。
// - 启动时清理旧版本遗留在 localStorage 的 nowen-auth 键。
if (typeof window !== 'undefined') {
  try {
    window.localStorage.removeItem('nowen-auth')
  } catch {
    /* ignore */
  }
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      isAuthenticated: false,

      setAuth: (token, user) =>
        set({ token, user, isAuthenticated: true }),

      logout: () =>
        set({ token: null, user: null, isAuthenticated: false }),

      updateUser: (user) =>
        set({ user }),
    }),
    {
      name: 'nowen-auth', // sessionStorage key
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({
        token: state.token,
        user: state.user,
        isAuthenticated: state.isAuthenticated,
      }),
    }
  )
)
