import type { ReactNode } from 'react'
import { Clapperboard } from 'lucide-react'
import { Tag } from '@/components/design-system'

interface AuthShellProps {
  title: ReactNode
  description?: ReactNode
  icon?: ReactNode
  eyebrow?: ReactNode
  children: ReactNode
  footer?: ReactNode
}

// 认证页外壳：环境光背景 + 桌面端品牌氛围区 + 玻璃质感表单卡片。
// 移动端为单列卡片布局，桌面端（≥1024px）左右分栏。
export default function AuthShell({ title, description, icon, eyebrow, children, footer }: AuthShellProps) {
  return (
    <div className="nv-auth-root">
      <div className="nv-auth-ambient" aria-hidden="true">
        <span className="nv-auth-glow nv-auth-glow-a" />
        <span className="nv-auth-glow nv-auth-glow-b" />
        <span className="nv-auth-glow nv-auth-glow-c" />
        <span className="nv-auth-gridlines" />
      </div>

      <div className="nv-auth-layout">
        <aside className="nv-auth-brand" aria-hidden="true">
          <div className="nv-auth-watermark">FAN-VIDEO</div>
          <div className="nv-auth-watermark-sub">Movie · Series · Workspace</div>
          <div className="nv-auth-stills">
            <span className="nv-auth-still nv-auth-still-a" />
            <span className="nv-auth-still nv-auth-still-b" />
            <span className="nv-auth-still nv-auth-still-c" />
          </div>
        </aside>

        <main className="nv-auth-panel">
          <section className="nv-auth-card">
            <header className="nv-auth-card-head">
              <div className="nv-auth-mark-row">
                <span className="nv-auth-mark">{icon ?? <Clapperboard size={20} aria-hidden="true" />}</span>
                {eyebrow && <Tag>{eyebrow}</Tag>}
              </div>
              <h1 className="nv-auth-title">{title}</h1>
              {description && <p className="nv-auth-desc">{description}</p>}
            </header>

            <div className="nv-auth-form">{children}</div>

            {footer && <div className="nv-auth-foot">{footer}</div>}
          </section>
        </main>
      </div>
    </div>
  )
}
