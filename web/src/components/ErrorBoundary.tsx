import { Component, type ErrorInfo, type ReactNode } from 'react'
import { AlertTriangle, Home, RefreshCw } from 'lucide-react'

interface ErrorBoundaryProps {
  children: ReactNode
}

interface ErrorBoundaryState {
  error: Error | null
}

export default class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary] 页面渲染异常', error, info.componentStack)
  }

  private reset = () => {
    this.setState({ error: null })
    window.location.href = '/'
  }

  render() {
    if (!this.state.error) return this.props.children

    return (
      <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 px-6 py-16 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full border border-[var(--nv-border-subtle)] bg-[var(--nv-bg-surface-soft)]">
          <AlertTriangle size={24} className="text-[var(--nv-status-warning)]" aria-hidden="true" />
        </div>
        <div>
          <h2 className="text-base font-semibold text-[var(--nv-text-primary)]">页面出错了</h2>
          <p className="mt-1 text-sm text-[var(--nv-text-tertiary)]">渲染过程中发生异常，请重试或返回首页。</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={this.reset}
            className="inline-flex items-center gap-2 rounded-[var(--nv-radius-control)] bg-[var(--nv-action-primary)] px-4 py-2 text-sm font-medium text-[var(--nv-color-on-primary)] transition-colors hover:opacity-90"
          >
            <Home size={16} aria-hidden="true" />
            返回首页
          </button>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="inline-flex items-center gap-2 rounded-[var(--nv-radius-control)] border border-[var(--nv-border-default)] px-4 py-2 text-sm font-medium text-[var(--nv-text-primary)] transition-colors hover:bg-[var(--nv-bg-interactive)]"
          >
            <RefreshCw size={16} aria-hidden="true" />
            刷新页面
          </button>
        </div>
      </div>
    )
  }
}