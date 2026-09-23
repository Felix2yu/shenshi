import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'

import { BoardView } from './components/BoardView'
import { CalendarView } from './components/CalendarView'
import { HabitsView } from './components/HabitsView'
import { QuadrantView } from './components/QuadrantView'
import { Sidebar } from './components/Sidebar'
import { StatsView } from './components/StatsView'
import { TableView } from './components/TableView'
import { TaskDetail } from './components/TaskDetail'
import { IconList, IconX, SealLogo } from './components/icons'
import { AppOverlays } from './components/Overlays'
import { TaskListView } from './components/TaskViews'
import { Toolbar } from './components/Toolbar'
import { Button, IconButton } from './components/ui'
import { applyFilter } from './lib/filter'
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

  const openTask = useCallback((t: Task) => setSelectedTask(t.id), [setSelectedTask])
  const closeTask = useCallback(() => setSelectedTask(null), [setSelectedTask])

  const visible = useMemo(() => applyFilter(tasks, filters), [tasks, filters])
  const emptyKey = emptyKeyFor(selection)

  // 切视图 / 清单时收起移动端抽屉，避免遮住内容
  useEffect(() => {
    setNavOpen(false)
  }, [selection, view])

  // 全局快捷键（与设置面板中的说明保持一致）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      const typing =
        target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)

      if (e.key === 'Escape') {
        if (selectedTaskId !== null) {
          closeTask()
          return
        }
        if (multiSelect) {
          clearSelected()
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
        <p className="brand-serif text-[15px] tracking-[0.14em] text-ink-2">慎始而敬终</p>
        <p className="text-[12px] text-ink-3">正在整理今日的案头…</p>
      </div>
    )
  }

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-paper text-ink">
      {/* 清单树：桌面常驻，移动端为抽屉 */}
      <div
        className={
          navOpen
            ? 'fixed inset-y-0 left-0 z-40 shadow-[var(--shadow-lg)] md:static md:z-auto md:shadow-none'
            : 'hidden h-full md:block'
        }
      >
        <Sidebar />
      </div>
      {navOpen ? (
        <div className="fixed inset-0 z-30 bg-ink/25 backdrop-blur-[1px] md:hidden" onClick={() => setNavOpen(false)} />
      ) : null}

      {/* 主区 */}
      <main className="flex min-w-0 flex-1 flex-col">
        {/* 移动端顶栏 */}
        <div className="flex items-center gap-2 border-b border-line bg-paper/90 px-3 py-2 md:hidden">
          <IconButton icon={IconList} label="清单" onClick={() => setNavOpen(true)} />
          <SealLogo size={22} />
          <span className="brand-serif text-[14.5px] font-medium">慎始</span>
          {selectedTaskId !== null ? (
            <IconButton icon={IconX} label="关闭详情" onClick={closeTask} className="ml-auto" />
          ) : null}
        </div>

        <Toolbar filters={filters} onFilters={setFilters} />

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

        <footer className="hidden shrink-0 items-center gap-3 border-t border-line px-5 py-1.5 text-[10.5px] text-ink-3 md:flex">
          <span>{boot?.app ?? '慎始'} · {boot?.motto ?? '慎始而敬终，行稳致远'}</span>
          <span className="ml-auto tabular-nums">
            {visible.filter((t) => t.status === 'todo').length} 待办 / 共 {visible.length} 项
          </span>
          <span className="text-ink-3/70">按 / 搜索 · Esc 退出</span>
        </footer>
      </main>

      {/* 任务详情：桌面右栏，移动端整屏浮层 */}
      {selectedTaskId !== null ? (
        <>
          <div className="fixed inset-0 z-30 bg-ink/20 md:hidden" onClick={closeTask} />
          <div className="fixed inset-y-0 right-0 z-40 w-full max-w-[400px] md:static md:z-auto md:w-auto md:max-w-none">
            <TaskDetail taskId={selectedTaskId} onClose={closeTask} />
          </div>
        </>
      ) : null}

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
        <h1 className="brand-serif text-center text-[17px] font-semibold tracking-[0.14em]">慎始而敬终</h1>
        <p className="mt-1.5 mb-5 text-center text-[12.5px] text-ink-3">访问口令已失效，请重新输入</p>

        <label className="mb-1.5 block text-[11.5px] font-medium tracking-wide text-ink-3" htmlFor="shenshi-token">
          访问口令
        </label>
        <input
          id="shenshi-token"
          type="password"
          autoFocus
          autoComplete="current-password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          className="w-full rounded-lg border border-line bg-surface px-2.5 py-2 text-[14px] text-ink outline-none transition-colors focus:border-seal/60"
        />

        <Button type="submit" variant="primary" disabled={busy || !token.trim()} className="mt-3 w-full">
          {busy ? '正在验证…' : '进入'}
        </Button>
        <p className="mt-2 min-h-[1.2rem] text-center text-[12px] text-p-high">{error}</p>
        <p className="mt-4 text-center text-[11px] text-ink-3">口令由服务端环境变量 SHENSHI_TOKEN 设定</p>
      </form>
    </div>
  )
}

function emptyKeyFor(selection: Selection): string {  switch (selection.kind) {
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
