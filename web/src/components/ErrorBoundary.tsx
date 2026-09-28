import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

/**
 * 全站最后防线：渲染期抛错（典型如 hook 条件调用、异步回写脏数据）时展示降级界面，
 * 而不是整页白屏。仍需按 `docs/a11y-regression-checklist.md` 修根因 —— 这里只兜底。
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // 留在控制台便于排查；上报通道接入后可在此扩展。
    console.error('渲染出错，已降级：', error, info.componentStack)
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div
        role="alert"
        className="flex h-full min-h-[60vh] flex-col items-center justify-center gap-3 p-8 text-center"
      >
        <p className="text-[0.9375rem] text-ink-2">页面渲染出错，已停止加载以免白屏。</p>
        <p className="max-w-[40rem] break-all font-mono text-[0.6875rem] text-ink-3">{error.message}</p>
        <button
          type="button"
          className="rounded-md border border-control-line bg-surface px-3 py-1.5 text-[0.8125rem] text-ink-2 hover:bg-surface-2"
          onClick={() => window.location.reload()}
        >
          重新加载
        </button>
      </div>
    )
  }
}
