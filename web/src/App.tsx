import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'

import { BoardView } from './components/BoardView'
import { CalendarView } from './components/CalendarView'
import { HabitsView } from './components/HabitsView'
import { QuadrantView } from './components/QuadrantView'
import { Sidebar } from './components/Sidebar'
import { StatsView } from './components/StatsView'
import { TableView } from './components/TableView'
import { TaskDetail } from './components/TaskDetail'
import { SealLogo } from './components/icons'
import { AppOverlays } from './components/Overlays'
import { BatchBar, TaskListView } from './components/TaskViews'
import { Toolbar } from './components/Toolbar'
import { Button } from './components/ui'
import { applyFilter } from './lib/filter'
import { useEscapeArbiter } from './lib/escStack'
import { useModalLayerActive } from './lib/modalLayer'
import { useStore } from './store/AppStore'
import type { Selection, Task, ViewKind } from './types'

/**
 * 「慎始」主装配。
 * 布局：左侧清单树 · 中间视图 · 右侧任务详情；晨省/日省/专注/提醒等覆盖层统一挂在最末。
 * 筛选状态收敛在 store 里（「保存的筛选」需要能直接改写它），此处只负责把它交给各视图。
 */
export default function App() {
  const {
    loading,
    boot,
    tasks,
    view,
    setView,
    selection,
    selectSmart,
    selectedTaskId,
    setSelectedTask,
    multiSelect,
    clearSelected,
    locked,
    filters,
    setFilters,
  } = useStore()
  const [navOpen, setNavOpen] = useState(false)
  // 窄屏（<lg，1024px）判定：抽屉的对话框语义只在这一档生效；
  // 768–1023px 的窄窗口/平板竖屏也归入移动档 —— 常驻侧栏会吃掉约四成宽度，
  // 主区被挤压、信息密度过低（2026-09-24 用户截图确认）。桌面档 ≥1024px。
  const isMobile = useMediaQuery('(max-width: 1023px)')
  const drawerRef = useRef<HTMLDivElement>(null)

  // document 上唯一的 Esc 仲裁器：让浮层之间的 Esc 处理"最上层优先"
  useEscapeArbiter()
  // 有弹窗打开时把背景标记为 inert
  const modalOpen = useModalLayerActive()

  const openTask = useCallback((t: Task) => setSelectedTask(t.id), [setSelectedTask])
  const closeTask = useCallback(() => setSelectedTask(null), [setSelectedTask])

  const visible = useMemo(() => applyFilter(tasks, filters), [tasks, filters])
  const emptyKey = emptyKeyFor(selection)

  // 切视图 / 清单时收起移动端抽屉，避免遮住内容
  useEffect(() => {
    setNavOpen(false)
  }, [selection, view])

  // 视口回到桌面宽度（≥lg）时抽屉语义失效，收掉状态，避免残留 lg:static 的"幽灵对话框"
  useEffect(() => {
    if (!isMobile) setNavOpen(false)
  }, [isMobile])

  // 打开抽屉时把焦点移进去（对话框惯例），读屏用户能立刻感知"进入了导航层"
  useEffect(() => {
    if (navOpen && isMobile) drawerRef.current?.focus()
  }, [navOpen, isMobile])

  // 全局快捷键（与设置面板中的说明保持一致）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      const typing =
        target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)

      if (e.key === 'Escape') {
        // 输入中的 Esc 归输入组件自己处理（取消候选 / 还原草稿），不触发全局上下文动作。
        // 浮层（Modal / Popover）已在各自的层里消费并 stopPropagation，不会走到这里。
        if (typing) return
        if (multiSelect) {
          clearSelected()
          return
        }
        if (selectedTaskId !== null) {
          closeTask()
          return
        }
        setNavOpen(false)
        return
      }

      // 带修饰键的组合交给浏览器
      if (typing || e.metaKey || e.ctrlKey || e.altKey) return

      const key = e.key.toLowerCase()
      if (key === '/') {
        e.preventDefault()
        document.getElementById('shenshi-search')?.focus()
        return
      }

      const goView: Record<string, ViewKind> = { c: 'calendar', q: 'quadrant', b: 'board', s: 'stats', h: 'habits' }
      if (key in goView) {
        e.preventDefault()
        setView(goView[key])
        return
      }

      if (key === 't' || key === 'i') {
        e.preventDefault()
        selectSmart(key === 't' ? 'today' : 'inbox')
        setView('list')
        return
      }

      if (key === 'n') {
        e.preventDefault()
        setView('list')
        // 等列表渲染完成后再聚焦输入框
        window.setTimeout(() => document.getElementById('shenshi-quickadd')?.focus(), 0)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectedTaskId, closeTask, multiSelect, clearSelected, setView, selectSmart])

  if (locked) {
    return <LockScreen />
  }

  if (loading) {
    return (
      <div className="flex h-dvh w-full flex-col items-center justify-center gap-4 bg-paper text-ink">
        <SealLogo size={52} />
        <p className="brand-serif text-[0.9375rem] tracking-[0.14em] text-ink-2">慎始而敬终</p>
        <p className="text-[0.75rem] text-ink-3">正在整理今日的案头…</p>
      </div>
    )
  }

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-paper text-ink">
      {/* 有弹窗打开时让背景整体 inert：不可聚焦、也不被读屏读到。
          弹窗已 portal 到 body，不在此子树内，因此不受影响；
          提醒中心 / 专注指示 / Toast 等自绘浮层留在外面，仍可交互。 */}
      <div className="contents" inert={modalOpen}>
      {/* 清单树：桌面（≥lg）常驻，窄屏为抽屉。
          抽屉态按对话框语义暴露给读屏（role=dialog + aria-modal），
          桌面常驻态不带任何角色，就是普通布局块。 */}
      <div
        id="shenshi-nav-drawer"
        ref={drawerRef}
        tabIndex={-1}
        role={navOpen && isMobile ? 'dialog' : undefined}
        aria-modal={navOpen && isMobile ? true : undefined}
        aria-label="清单导航"
        className={
          navOpen
            ? 'fixed inset-y-0 left-0 z-40 shadow-[var(--shadow-lg)] outline-none lg:static lg:z-auto lg:shadow-none'
            : 'hidden h-full lg:block'
        }
      >
        <Sidebar onCloseRequest={() => setNavOpen(false)} />
      </div>
      {navOpen ? (
        <div
          className="fixed inset-0 z-30 bg-ink/25 backdrop-blur-[1px] lg:hidden"
          onClick={() => setNavOpen(false)}
          aria-hidden="true"
        />
      ) : null}

      {/* 主区 */}
      <main className="flex min-w-0 flex-1 flex-col">
        {/* 窄屏的汉堡入口直接并入工具栏标题行（见 Toolbar），不再单设一条顶栏 ——
            顶部 chrome 少占一行，标题/搜索/页签全部留在一条紧凑头部里。 */}

        <Toolbar filters={filters} onFilters={setFilters} onOpenNav={() => setNavOpen(true)} />
        {/* 多选批处理条全局挂一份：各视图（含看板/表格/四象限）进入多选后都能操作 */}
        <BatchBar />

        <div
          className={
            view === 'list' || view === 'table' ? 'min-h-0 flex-1 overflow-y-auto' : 'min-h-0 flex-1 overflow-hidden'
          }
        >
          {view === 'list' ? (
            <TaskListView tasks={visible} onOpen={openTask} emptyKey={emptyKey} />
          ) : view === 'table' ? (
            <TableView tasks={visible} onOpen={openTask} />
          ) : view === 'board' ? (
            <BoardView onOpen={openTask} filter={filters} />
          ) : view === 'calendar' ? (
            <CalendarView onOpen={openTask} filter={filters} />
          ) : view === 'quadrant' ? (
            <QuadrantView onOpen={openTask} filter={filters} />
          ) : view === 'habits' ? (
            <HabitsView />
          ) : (
            <StatsView />
          )}
        </div>

        {/* data-app-footer：Popover 量测时要把这条常驻状态栏从可用高度里扣掉，
            否则浮层向下展开停在它下面等于被遮住（见 ui.tsx 的 Popover）。
            移动端此处为 hidden（display:none），offsetHeight 读作 0，逻辑自然适配。 */}
        <footer
          data-app-footer
          className="hidden shrink-0 items-center gap-3 border-t border-line px-5 py-1.5 text-[0.65625rem] text-ink-3 lg:flex"
        >
          <span>{boot?.app ?? '慎始'} · {boot?.motto ?? '慎始而敬终，行稳致远'}</span>
          <span className="ml-auto tabular-nums">
            {visible.filter((t) => t.status !== 'done').length} 待办 / 共 {visible.length} 项
          </span>
          <span className="text-ink-3/70">按 / 搜索 · Esc 退出</span>
        </footer>
      </main>

      {/* 任务详情：桌面（≥lg）右栏，窄屏整屏浮层（自带关闭按钮，无需顶栏兜底） */}
      {selectedTaskId !== null ? (
        <>
          <div className="fixed inset-0 z-30 bg-ink/20 lg:hidden" onClick={closeTask} aria-hidden="true" />
          <div
            role="dialog"
            aria-modal="true"
            aria-label="任务详情"
            className="fixed inset-y-0 right-0 z-40 w-full max-w-[400px] lg:static lg:z-auto lg:w-auto lg:max-w-none"
          >
            <TaskDetail taskId={selectedTaskId} onClose={closeTask} />
          </div>
        </>
      ) : null}

      </div>
      <AppOverlays />
    </div>
  )
}

/**
 * 会话失效后的解锁界面。
 * 与服务端那张独立登录页是一回事：服务端负责「根本没进来」的场景，
 * 这里负责「已经开着页面、中途失效」的场景，两者说辞保持一致。
 */
function LockScreen() {
  const { unlock } = useStore()
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const value = token.trim()
    if (!value || busy) return
    setBusy(true)
    setError('')
    const ok = await unlock(value)
    if (!ok) {
      setError('口令不正确')
      setBusy(false)
    }
  }

  return (
    <div
      data-lock-screen
      className="grid h-dvh w-full place-items-center bg-paper px-4 text-ink"
    >
      <form onSubmit={submit} className="w-full max-w-[21rem]">
        <div className="mb-4 flex justify-center">
          <SealLogo size={44} />
        </div>
        <h1 className="brand-serif text-center text-[1.0625rem] font-semibold tracking-[0.14em]">慎始而敬终</h1>
        <p className="mt-1.5 mb-5 text-center text-[0.78125rem] text-ink-3">访问口令已失效，请重新输入</p>

        <label className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3" htmlFor="shenshi-token">
          访问口令
        </label>
        <input
          id="shenshi-token"
          type="password"
          autoFocus
          autoComplete="current-password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          className="w-full rounded-lg border border-line bg-surface px-2.5 py-2 text-[0.875rem] text-ink outline-none transition-colors focus:border-seal/60"
        />

        <Button type="submit" variant="primary" disabled={busy || !token.trim()} className="mt-3 w-full">
          {busy ? '正在验证…' : '进入'}
        </Button>
        <p className="mt-2 min-h-[1.2rem] text-center text-[0.75rem] text-p-high">{error}</p>
        <p className="mt-4 text-center text-[0.6875rem] text-ink-3">口令由服务端环境变量 SHENSHI_TOKEN 设定</p>
      </form>
    </div>
  )
}

function emptyKeyFor(selection: Selection): string {
  switch (selection.kind) {
    case 'smart':
      return selection.key
    case 'list':
      return 'list'
    case 'folder':
      return 'folder'
    case 'tag':
      return 'tag'
    case 'search':
      return 'search'
  }
}

/** 响应式媒体查询钩子：SSR/无窗口环境安全降级为 false。 */
function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' && 'matchMedia' in window ? window.matchMedia(query).matches : false,
  )
  useEffect(() => {
    const mq = window.matchMedia(query)
    const onChange = () => setMatches(mq.matches)
    onChange()
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [query])
  return matches
}
