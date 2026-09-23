import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'

import { ApiError, api, setUnauthorizedHandler, type TaskQuery, type TaskSort } from '../api/client'
import { todayStr } from '../lib/date'
import { EMPTY_FILTER, filterFromQuery, filterToQuery, isFilterActive, type TaskFilter } from '../lib/filter'
import { playChime, playTick, pushNotification } from '../lib/notify'
import type {
  Activity,
  BatchAction,
  Bootstrap,
  DailyFocus,
  Folder,
  List,
  ReminderHit,
  RepeatMeta,
  SavedFilter,
  Selection,
  Settings,
  SmartKey,
  Stats,
  Tag,
  Task,
  TaskPatch,
  UndoState,
  ViewKind,
} from '../types'

export interface Toast {
  id: number
  kind: 'ok' | 'error' | 'info'
  message: string
}

interface ConfirmState {
  id: number
  title: string
  message: string
  confirmText: string
  danger: boolean
  resolve: (ok: boolean) => void
}

export interface FocusState {
  taskId: number | null
  minutes: number
  running: boolean
  endsAt: number | null
  remaining: number
}

interface StoreShape {
  loading: boolean
  boot: Bootstrap | null
  tasks: Task[]
  tasksLoading: boolean
  selection: Selection
  view: ViewKind
  keyword: string
  /** 工具栏筛选条件。收敛在 store 里，是为了让「保存的筛选」这类入口能直接改写它。 */
  filters: TaskFilter
  settings: Settings
  toasts: Toast[]
  reminders: ReminderHit[]
  stats: Stats | null
  repeatMeta: RepeatMeta | null
  selectedTaskId: number | null
  selectedIds: number[]
  multiSelect: boolean
  focus: FocusState
  confirmState: ConfirmState | null
  folders: Folder[]
  lists: List[]
  tags: Tag[]
  counts: Record<string, number>
  todayFocusIds: number[]
  savedFilters: SavedFilter[]
  /** 最近一次删除是否还能挽回；null 表示尚未问过服务端。 */
  undo: UndoState | null
  activities: Activity[]
  activitiesLoaded: boolean
  /** 每次写操作对账后自增。日历、统计等自持数据的视图据此重新拉取。 */
  version: number

  select: (s: Selection) => void
  selectSmart: (key: SmartKey) => void
  setView: (v: ViewKind) => void
  setKeyword: (k: string) => void
  setFilters: (f: TaskFilter) => void
  resetFilters: () => void
  setSelectedTask: (id: number | null) => void
  setMultiSelect: (on: boolean) => void
  toggleSelected: (id: number) => void
  clearSelected: () => void

  refreshTasks: () => Promise<void>
  refreshBoot: () => Promise<void>
  reconcile: () => void

  createTask: (patch: TaskPatch) => Promise<Task | null>
  updateTask: (id: number, patch: TaskPatch) => Promise<Task | null>
  deleteTask: (id: number) => Promise<void>
  toggleTask: (id: number) => Promise<{ task: Task; nextTask: Task | null } | null>
  moveTask: (id: number, body: { listId?: number; dueDate?: string | null; dueTime?: string | null }) => Promise<void>
  skipTask: (id: number) => Promise<void>
  /** 复制一份任务：结构照搬，状态归零。 */
  duplicateTask: (id: number) => Promise<Task | null>
  batch: (action: BatchAction, extra?: { listId?: number; dueDate?: string }) => Promise<void>
  /**
   * 清空已完成任务。传 listId 表示只清该清单内的。
   * 这会把「剩多少件」直接归零，调用方必须先做过二次确认。
   */
  purgeCompleted: (listId?: number) => Promise<number>
  reorderTasks: (ids: number[]) => Promise<void>

  /** 把最近一次删除恢复回来。 */
  undoDelete: () => Promise<void>
  /** 主动放弃撤销机会：此刻附件才真正从磁盘上消失。 */
  dropUndo: () => Promise<void>

  loadActivities: () => Promise<void>
  clearActivities: () => Promise<void>

  saveFilter: (name: string) => Promise<SavedFilter | null>
  applySavedFilter: (f: SavedFilter) => void
  renameSavedFilter: (id: number, name: string) => Promise<void>
  deleteSavedFilter: (id: number) => Promise<void>

  /** 启用访问口令后，会话失效时置为 true，由 App 渲染解锁界面。 */
  locked: boolean
  /** 用口令换取会话；成功即整页重载，把失效期间的请求补回来。 */
  unlock: (token: string) => Promise<boolean>

  addSubtask: (taskId: number, title: string) => Promise<void>
  updateSubtask: (id: number, patch: { title?: string; done?: boolean }) => Promise<void>
  deleteSubtask: (id: number) => Promise<void>

  createFolder: (name: string, color?: string, parentId?: number) => Promise<Folder | null>
  updateFolder: (
    id: number,
    patch: Partial<{
      name: string
      color: string
      collapsed: boolean
      archived: boolean
      parentId: number | null
      moveToRoot: boolean
    }>,
  ) => Promise<void>
  deleteFolder: (id: number) => Promise<void>
  reorderFolders: (ids: number[]) => Promise<void>
  createList: (name: string, folderId?: number, color?: string) => Promise<List | null>
  updateList: (
    id: number,
    patch: Partial<{
      name: string
      color: string
      icon: string
      folderId: number
      moveToRoot: boolean
      archived: boolean
      starred: boolean
    }>,
  ) => Promise<void>
  deleteList: (id: number) => Promise<void>
  reorderLists: (ids: number[]) => Promise<void>

  ensureTags: (names: string[]) => Promise<Tag[]>
  createTag: (name: string, color?: string) => Promise<Tag | null>
  updateTag: (id: number, patch: { name?: string; color?: string }) => Promise<void>
  deleteTag: (id: number) => Promise<void>

  saveSettings: (patch: Settings) => Promise<void>
  sortBy: TaskSort
  setSortBy: (v: TaskSort) => void
  toast: (message: string, kind?: Toast['kind']) => void
  dismissToast: (id: number) => void

  dismissReminder: (key: string) => void
  snoozeReminder: (hit: ReminderHit, minutes: number) => void

  loadStats: (days?: number) => Promise<void>
  setTodayFocus: (ids: number[]) => Promise<void>

  startFocus: (taskId: number | null, minutes: number) => void
  stopFocus: (completed: boolean) => Promise<void>
  tickFocus: () => void

  confirm: (opts: { title: string; message: string; confirmText?: string; danger?: boolean }) => Promise<boolean>
  resolveConfirm: (ok: boolean) => void
}

const Ctx = createContext<StoreShape | null>(null)

export function useStore(): StoreShape {
  const v = useContext(Ctx)
  if (!v) throw new Error('useStore 必须在 AppProvider 内部使用')
  return v
}

const DEFAULT_SELECTION: Selection = { kind: 'smart', key: 'today' }

function queryFor(selection: Selection, keyword: string, sortBy: TaskSort): TaskQuery {
  if (keyword.trim()) return { q: keyword.trim(), status: 'all', sortBy }
  switch (selection.kind) {
    case 'smart':
      return { smart: selection.key, sortBy }
    case 'folder':
      return { folderId: selection.id, sortBy }
    case 'list':
      return { listId: selection.id, sortBy }
    case 'tag':
      return { tagId: selection.id, sortBy }
    case 'search':
      return { q: selection.q, status: 'all', sortBy }
  }
}

/**
 * 把 list 中出现在 ids 里的元素按 ids 的顺序摆回它们原本占用的位置，其余元素保持不动。
 * 用于拖拽排序的乐观更新——只重排被拖动的那一批，不扰乱其他任务与分组的相对次序。
 */
function reorderById<T extends { id: number }>(list: T[], ids: number[]): T[] {
  const pos = new Map(ids.map((id, i) => [id, i]))
  const slots: number[] = []
  list.forEach((item, i) => {
    if (pos.has(item.id)) slots.push(i)
  })
  if (slots.length < 2) return list
  const picked = slots.map((i) => list[i]).sort((a, b) => (pos.get(a.id) ?? 0) - (pos.get(b.id) ?? 0))
  const next = [...list]
  slots.forEach((slot, k) => {
    next[slot] = picked[k]
  })
  return next
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [boot, setBoot] = useState<Bootstrap | null>(null)
  const [tasks, setTasks] = useState<Task[]>([])
  const [tasksLoading, setTasksLoading] = useState(false)
  const [selection, setSelection] = useState<Selection>(DEFAULT_SELECTION)
  const [view, setView] = useState<ViewKind>('list')
  const [keyword, setKeywordState] = useState('')
  const [filters, setFiltersState] = useState<TaskFilter>(EMPTY_FILTER)
  const [settings, setSettings] = useState<Settings>({})
  // 排序方式随设置持久化。默认「智能」，由到期日与优先级主导；
  // 只有切到「手动」时，用户拖拽出的 sort_order 才会真正决定顺序。
  const sortBy: TaskSort = (settings.sortBy as TaskSort) || 'smart'
  const [toasts, setToasts] = useState<Toast[]>([])
  const [reminders, setReminders] = useState<ReminderHit[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [repeatMeta, setRepeatMeta] = useState<RepeatMeta | null>(null)
  const [savedFilters, setSavedFilters] = useState<SavedFilter[]>([])
  const [undo, setUndo] = useState<UndoState | null>(null)
  const [activities, setActivities] = useState<Activity[]>([])
  const [activitiesLoaded, setActivitiesLoaded] = useState(false)
  const [selectedTaskId, setSelectedTaskId] = useState<number | null>(null)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [multiSelect, setMultiSelect] = useState(false)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [locked, setLocked] = useState(false)
  const [version, setVersion] = useState(0)
  const [focus, setFocus] = useState<FocusState>({
    taskId: null,
    minutes: 25,
    running: false,
    endsAt: null,
    remaining: 25 * 60,
  })

  const toastSeq = useRef(0)
  const confirmSeq = useRef(0)
  const reconcileTimer = useRef<number | null>(null)
  const seenReminders = useRef<Set<string>>(new Set())
  const snoozeTimers = useRef<number[]>([])

  const toast = useCallback((message: string, kind: Toast['kind'] = 'ok') => {
    const id = ++toastSeq.current
    setToasts((prev) => [...prev, { id, kind, message }])
    window.setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), kind === 'error' ? 5200 : 3200)
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const handleError = useCallback(
    (e: unknown, fallback = '操作失败') => {
      const msg = e instanceof ApiError ? e.message : fallback
      toast(msg, 'error')
    },
    [toast],
  )

  // 服务端启用访问口令后，任何 401 都直接把界面切到解锁态，
  // 而不是让各个视图各自弹一条「请求失败」。
  useEffect(() => {
    setUnauthorizedHandler(() => setLocked(true))
    return () => setUnauthorizedHandler(null)
  }, [])

  const unlock = useCallback(async (token: string) => {
    try {
      await api.login(token)
    } catch {
      return false
    }
    setLocked(false)
    // 整页重载：会话失效期间失败的那些请求，靠逐个补拉不划算。
    window.location.reload()
    return true
  }, [])

  const refreshBoot = useCallback(async () => {
    try {
      const b = await api.bootstrap()
      setBoot(b)
      setSettings((prev) => ({ ...prev, ...b.settings }))
      // 角标与撤销槽位同批返回：删除之后不需要额外一次请求，提示条就能亮起来。
      setSavedFilters(b.savedFilters ?? [])
      setUndo(b.undo ?? null)
    } catch (e) {
      handleError(e, '加载基础数据失败')
    }
  }, [handleError])

  const refreshTasks = useCallback(async () => {
    setTasksLoading(true)
    try {
      const r = await api.listTasks(queryFor(selection, keyword, sortBy))
      setTasks(r.tasks)
    } catch (e) {
      handleError(e, '加载任务失败')
    } finally {
      setTasksLoading(false)
    }
  }, [selection, keyword, sortBy, handleError])

  /** 写操作后统一做一次去抖对账：刷新角标、必要时刷新列表。 */
  const reconcile = useCallback(() => {
    if (reconcileTimer.current) window.clearTimeout(reconcileTimer.current)
    reconcileTimer.current = window.setTimeout(() => {
      void refreshBoot()
      void refreshTasks()
      setVersion((v) => v + 1)
    }, 220)
  }, [refreshBoot, refreshTasks])

  const loadStats = useCallback(async (days = 30) => {
    try {
      setStats(await api.stats(days))
    } catch (e) {
      handleError(e, '加载统计失败')
    }
  }, [handleError])

  // 首次加载
  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const [b, meta] = await Promise.all([api.bootstrap(), api.repeatMeta()])
        if (!alive) return
        setBoot(b)
        setRepeatMeta(meta)
        setSavedFilters(b.savedFilters ?? [])
        setUndo(b.undo ?? null)
        // 未设置过外观时，跟随系统偏好，避免第一次打开就与系统主题相逆。
        const stored = b.settings ?? {}
        const theme: 'light' | 'dark' =
          stored.theme === 'dark' || stored.theme === 'light'
            ? stored.theme
            : window.matchMedia?.('(prefers-color-scheme: dark)').matches
              ? 'dark'
              : 'light'
        setSettings({ ...stored, theme, accent: stored.accent || 'seal' })
      } catch (e) {
        if (alive) handleError(e, '无法连接到服务')
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [handleError])

  // 视图 / 选择变化时拉取任务
  useEffect(() => {
    if (loading) return
    void refreshTasks()
  }, [loading, refreshTasks])

  // 主题与外观
  useEffect(() => {
    const root = document.documentElement
    const theme = settings.theme ?? 'light'
    root.dataset.theme = theme
    root.dataset.accent = settings.accent || 'seal'
    root.style.colorScheme = theme
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', theme === 'dark' ? '#171513' : '#faf7f2')
  }, [settings.theme, settings.accent])

  // 周期刷新角标，让「今天 / 最近7天」的计数不因跨天而失真
  useEffect(() => {
    const t = window.setInterval(() => void refreshBoot(), 5 * 60 * 1000)
    return () => window.clearInterval(t)
  }, [refreshBoot])

  // 提醒轮询：每 30 秒问一次服务端「现在该提醒什么」。
  useEffect(() => {
    if (loading) return
    const poll = async () => {
      try {
        const r = await api.dueReminders(120)
        const fresh = r.reminders.filter((h) => !seenReminders.current.has(`${h.task.id}|${h.fireAt}`))
        if (!fresh.length) return
        for (const h of fresh) {
          seenReminders.current.add(`${h.task.id}|${h.fireAt}`)
          // 立即回执，避免多标签页重复提醒；本次仍保留在提醒中心供处理。
          void api.ackReminder(h.task.id, h.fireAt).catch(() => undefined)
          pushNotification(`慎始 · ${h.task.title}`, `${h.dueLabel}${h.overdue ? '（已到时间）' : ''}`)
        }
        if (settings.soundOn !== '0') playChime()
        setReminders((prev) => [...prev, ...fresh])
      } catch {
        /* 轮询失败静默，下个周期再试 */
      }
    }
    void poll()
    const t = window.setInterval(() => void poll(), 30_000)
    return () => window.clearInterval(t)
  }, [loading, settings.soundOn])

  // 撤销槽位带时效（10 分钟），提示条亮着的时候隔一会儿问一次，过期就自己收起来。
  useEffect(() => {
    if (!undo?.available) return
    const t = window.setInterval(() => {
      void api
        .undoState()
        .then(setUndo)
        .catch(() => undefined)
    }, 30_000)
    return () => window.clearInterval(t)
  }, [undo?.available])

  // 专注计时
  const tickFocus = useCallback(() => {
    setFocus((prev) => {
      if (!prev.running || !prev.endsAt) return prev
      const remaining = Math.max(0, Math.round((prev.endsAt - Date.now()) / 1000))
      return { ...prev, remaining }
    })
  }, [])

  useEffect(() => {
    if (!focus.running) return
    const t = window.setInterval(tickFocus, 1000)
    return () => window.clearInterval(t)
  }, [focus.running, tickFocus])

  const focusFinishRef = useRef<(completed: boolean) => Promise<void>>(async () => undefined)
  useEffect(() => {
    if (focus.running && focus.remaining === 0) {
      void focusFinishRef.current(true)
    }
  }, [focus.running, focus.remaining])

  const saveSettings = useCallback(
    async (patch: Settings) => {
      const next = { ...settings, ...patch }
      setSettings(next)
      try {
        const clean: Record<string, string> = {}
        for (const [k, v] of Object.entries(next)) if (typeof v === 'string') clean[k] = v
        await api.saveSettings(clean)
      } catch (e) {
        handleError(e, '保存设置失败')
      }
    },
    [settings, handleError],
  )

  const setSortBy = useCallback((v: TaskSort) => void saveSettings({ sortBy: v }), [saveSettings])

  // 排序方式真正变化时才重拉列表；首次由 bootstrap 流程负责，避免重复请求。
  const sortRef = useRef(sortBy)
  useEffect(() => {
    if (sortRef.current === sortBy) return
    sortRef.current = sortBy
    void refreshTasks()
  }, [sortBy, refreshTasks])

  // ---------- 拖拽排序 ----------

  /** 列表内拖拽排序：先乐观重排，再落库；失败则回滚重拉。 */
  const reorderTasks = useCallback(
    async (ids: number[]) => {
      if (ids.length < 2) return
      setTasks((prev) => reorderById(prev, ids))
      try {
        await api.reorderTasks(ids)
      } catch (e) {
        handleError(e, '排序失败')
        void refreshTasks()
      }
    },
    [handleError, refreshTasks],
  )

  /** 侧栏分组拖拽排序。 */
  const reorderFolders = useCallback(
    async (ids: number[]) => {
      if (ids.length < 2) return
      setBoot((prev) => (prev ? { ...prev, folders: reorderById(prev.folders, ids) } : prev))
      try {
        await api.reorderFolders(ids)
      } catch (e) {
        handleError(e, '排序失败')
        void refreshBoot()
      }
    },
    [handleError, refreshBoot],
  )

  /** 侧栏清单拖拽排序。 */
  const reorderLists = useCallback(
    async (ids: number[]) => {
      if (ids.length < 2) return
      setBoot((prev) => (prev ? { ...prev, lists: reorderById(prev.lists, ids) } : prev))
      try {
        await api.reorderLists(ids)
      } catch (e) {
        handleError(e, '排序失败')
        void refreshBoot()
      }
    },
    [handleError, refreshBoot],
  )

  // ---------- 任务写操作 ----------

  const patchLocalTask = useCallback((updated: Task) => {
    setTasks((prev) => prev.map((t) => (t.id === updated.id ? updated : t)))
  }, [])

  const createTask = useCallback(
    async (patch: TaskPatch) => {
      try {
        if (patch.listId === undefined && selection.kind === 'list') patch.listId = selection.id
        const t = await api.createTask(patch)
        toast(`已记下「${t.title}」`)
        reconcile()
        return t
      } catch (e) {
        handleError(e, '创建任务失败')
        return null
      }
    },
    [selection, toast, reconcile, handleError],
  )

  const updateTask = useCallback(
    async (id: number, patch: TaskPatch) => {
      try {
        const t = await api.updateTask(id, patch)
        patchLocalTask(t)
        reconcile()
        return t
      } catch (e) {
        handleError(e, '更新任务失败')
        return null
      }
    },
    [patchLocalTask, reconcile, handleError],
  )

  const deleteTask = useCallback(
    async (id: number) => {
      try {
        await api.deleteTask(id)
        setTasks((prev) => prev.filter((t) => t.id !== id))
        if (selectedTaskId === id) setSelectedTaskId(null)
        setSelectedIds((prev) => prev.filter((x) => x !== id))
        toast('已删除，左下角可撤销')
        reconcile()
      } catch (e) {
        handleError(e, '删除失败')
      }
    },
    [selectedTaskId, toast, reconcile, handleError],
  )

  const toggleTask = useCallback(
    async (id: number) => {
      const cur = tasks.find((t) => t.id === id)
      if (cur) {
        // 乐观翻转，让勾选零延迟；随后用服务端结果覆盖。
        setTasks((prev) =>
          prev.map((t) => (t.id === id ? { ...t, status: t.status === 'done' ? 'todo' : 'done' } : t)),
        )
      }
      try {
        const res = await api.toggleTask(id)
        patchLocalTask(res.task)
        if (res.completed) {
          if (settings.soundOn !== '0') playTick()
          if (res.nextTask) {
            toast(`已完成，下一次安排在 ${res.nextTask.dueDate ?? '—'}`)
          } else {
            toast('已完成 · 敬终')
          }
        }
        reconcile()
        return res
      } catch (e) {
        handleError(e, '操作失败')
        void refreshTasks()
        return null
      }
    },
    [tasks, patchLocalTask, settings.soundOn, toast, reconcile, handleError, refreshTasks],
  )

  const moveTask = useCallback(
    async (id: number, body: { listId?: number; dueDate?: string | null; dueTime?: string | null }) => {
      try {
        const t = await api.moveTask(id, body)
        patchLocalTask(t)
        reconcile()
      } catch (e) {
        handleError(e, '移动失败')
      }
    },
    [patchLocalTask, reconcile, handleError],
  )

  /** 跳过重复任务的本次发生：不记为完成，只把日期推到下一次。 */
  const skipTask = useCallback(
    async (id: number) => {
      try {
        const t = await api.skipTask(id)
        patchLocalTask(t)
        toast(t.dueDate ? `已跳过本次，下一次在 ${t.dueDate}` : '已跳过本次')
        reconcile()
      } catch (e) {
        handleError(e, '跳过失败')
        void refreshTasks()
      }
    },
    [patchLocalTask, toast, reconcile, handleError, refreshTasks],
  )

  const batch = useCallback(
    async (action: BatchAction, extra?: { listId?: number; dueDate?: string }) => {
      if (!selectedIds.length) return
      try {
        const r = await api.batch(selectedIds, action, extra)
        toast(`已处理 ${r.affected} 项`)
        setSelectedIds([])
        setMultiSelect(false)
        reconcile()
      } catch (e) {
        handleError(e, '批量操作失败')
      }
    },
    [selectedIds, toast, reconcile, handleError],
  )

  /** 复制任务：副本落在原任务之后，标题带「（副本）」以便一眼分辨。 */
  const duplicateTask = useCallback(
    async (id: number) => {
      try {
        const t = await api.duplicateTask(id)
        toast(`已复制为「${t.title}」`)
        reconcile()
        return t
      } catch (e) {
        handleError(e, '复制任务失败')
        return null
      }
    },
    [toast, reconcile, handleError],
  )

  /**
   * 清空已完成任务。只清当前清单还是清全部，由调用方决定并负责二次确认——
   * 这是一条真的会删数据的路，store 不该自作主张。
   */
  const purgeCompleted = useCallback(
    async (listId?: number) => {
      try {
        const r = await api.purgeCompleted(listId)
        if (r.affected > 0) toast(`已清空 ${r.affected} 项已完成任务`)
        else toast('没有可以清空的已完成任务', 'info')
        reconcile()
        return r.affected
      } catch (e) {
        handleError(e, '清空失败')
        return 0
      }
    },
    [toast, reconcile, handleError],
  )

  // ---------- 撤销与操作历史 ----------

  const undoDelete = useCallback(async () => {
    try {
      const r = await api.undo()
      toast(`已恢复 ${r.restored} 项`)
      reconcile()
    } catch (e) {
      handleError(e, '撤销失败')
      // 槽位可能刚好过期，重新问一次服务端把提示条收起来。
      void api
        .undoState()
        .then(setUndo)
        .catch(() => undefined)
    }
  }, [toast, reconcile, handleError])

  const dropUndo = useCallback(async () => {
    setUndo(null)
    try {
      await api.dropUndo()
    } catch {
      /* 放弃撤销不是关键路径，失败也不打扰用户 */
    }
  }, [])

  const loadActivities = useCallback(async () => {
    try {
      const r = await api.listActivities()
      setActivities(r.activities ?? [])
      setActivitiesLoaded(true)
    } catch (e) {
      handleError(e, '加载操作历史失败')
    }
  }, [handleError])

  const clearActivities = useCallback(async () => {
    try {
      await api.clearActivities()
      setActivities([])
    } catch (e) {
      handleError(e, '清空历史失败')
    }
  }, [handleError])

  // ---------- 保存的筛选条件 ----------

  const saveFilter = useCallback(
    async (name: string) => {
      const trimmed = name.trim()
      if (!trimmed) {
        toast('请给这组条件起个名字', 'error')
        return null
      }
      if (!isFilterActive(filters)) {
        toast('当前没有生效的筛选条件', 'info')
        return null
      }
      try {
        const f = await api.createSavedFilter({ name: trimmed, query: filterToQuery(filters) })
        setSavedFilters((prev) => [...prev, f])
        toast(`已保存筛选「${f.name}」`)
        return f
      } catch (e) {
        handleError(e, '保存筛选失败')
        return null
      }
    },
    [filters, toast, handleError],
  )

  const applySavedFilter = useCallback(
    (f: SavedFilter) => {
      setFiltersState(filterFromQuery(f.query))
      toast(`已套用筛选「${f.name}」`)
    },
    [toast],
  )

  const renameSavedFilter = useCallback(
    async (id: number, name: string) => {
      const trimmed = name.trim()
      if (!trimmed) return
      setSavedFilters((prev) => prev.map((f) => (f.id === id ? { ...f, name: trimmed } : f)))
      try {
        await api.updateSavedFilter(id, { name: trimmed })
      } catch (e) {
        handleError(e, '重命名失败')
        void refreshBoot()
      }
    },
    [handleError, refreshBoot],
  )

  const deleteSavedFilter = useCallback(
    async (id: number) => {
      setSavedFilters((prev) => prev.filter((f) => f.id !== id))
      try {
        await api.deleteSavedFilter(id)
      } catch (e) {
        handleError(e, '删除筛选失败')
        void refreshBoot()
      }
    },
    [handleError, refreshBoot],
  )

  // ---------- 子任务 ----------

  const addSubtask = useCallback(
    async (taskId: number, title: string) => {
      try {
        await api.addSubtask(taskId, title)
        const t = await api.getTask(taskId)
        patchLocalTask(t)
      } catch (e) {
        handleError(e, '新增子任务失败')
      }
    },
    [patchLocalTask, handleError],
  )

  const updateSubtask = useCallback(
    async (id: number, patch: { title?: string; done?: boolean }) => {
      try {
        await api.updateSubtask(id, patch)
        // 子任务归属父任务，重新取一次父任务即可拿到最新进度。
        const parent = tasks.find((t) => t.subtasks.some((s) => s.id === id))
        if (parent) patchLocalTask(await api.getTask(parent.id))
      } catch (e) {
        handleError(e, '更新子任务失败')
      }
    },
    [tasks, patchLocalTask, handleError],
  )

  const deleteSubtask = useCallback(
    async (id: number) => {
      try {
        await api.deleteSubtask(id)
        const parent = tasks.find((t) => t.subtasks.some((s) => s.id === id))
        if (parent) patchLocalTask(await api.getTask(parent.id))
      } catch (e) {
        handleError(e, '删除子任务失败')
      }
    },
    [tasks, patchLocalTask, handleError],
  )

  // ---------- 组织 ----------

  const createFolder = useCallback(
    async (name: string, color?: string, parentId?: number) => {
      try {
        const f = await api.createFolder({ name, color, parentId })
        await refreshBoot()
        toast(`已创建分组「${f.name}」`)
        return f
      } catch (e) {
        handleError(e, '创建分组失败')
        return null
      }
    },
    [refreshBoot, toast, handleError],
  )

  const updateFolder = useCallback(
    async (
      id: number,
      patch: Partial<{
        name: string
        color: string
        collapsed: boolean
        archived: boolean
        parentId: number | null
        moveToRoot: boolean
      }>,
    ) => {
      // 折叠是高频交互，先本地生效再落库。
      if (patch.collapsed !== undefined) {
        setBoot((prev) =>
          prev
            ? { ...prev, folders: prev.folders.map((f) => (f.id === id ? { ...f, collapsed: patch.collapsed! } : f)) }
            : prev,
        )
      }
      try {
        await api.updateFolder(id, patch)
        if (patch.collapsed === undefined) await refreshBoot()
      } catch (e) {
        handleError(e, '更新分组失败')
        await refreshBoot()
      }
    },
    [refreshBoot, handleError],
  )

  const deleteFolder = useCallback(
    async (id: number) => {
      try {
        await api.deleteFolder(id)
        if (selection.kind === 'folder' && selection.id === id) setSelection(DEFAULT_SELECTION)
        await refreshBoot()
        toast('分组已删除，其中清单与子分组提到上一层')
      } catch (e) {
        handleError(e, '删除分组失败')
      }
    },
    [selection, refreshBoot, toast, handleError],
  )

  const createList = useCallback(
    async (name: string, folderId?: number, color?: string) => {
      try {
        const l = await api.createList({ name, folderId, color })
        await refreshBoot()
        setSelection({ kind: 'list', id: l.id })
        toast(`已创建清单「${l.name}」`)
        return l
      } catch (e) {
        handleError(e, '创建清单失败')
        return null
      }
    },
    [refreshBoot, toast, handleError],
  )

  const updateList = useCallback(
    async (
      id: number,
      patch: Partial<{
        name: string
        color: string
        icon: string
        folderId: number
        moveToRoot: boolean
        archived: boolean
        starred: boolean
      }>,
    ) => {
      // 收藏是单点交互，先本地翻转再落库；失败时由 refreshBoot 拨回。
      if (patch.starred !== undefined || patch.archived !== undefined) {
        setBoot((prev) =>
          prev
            ? {
                ...prev,
                lists: prev.lists.map((l) =>
                  l.id === id
                    ? {
                        ...l,
                        starred: patch.starred ?? l.starred,
                        archived: patch.archived ?? l.archived,
                      }
                    : l,
                ),
              }
            : prev,
        )
      }
      try {
        await api.updateList(id, patch)
        await refreshBoot()
      } catch (e) {
        handleError(e, '更新清单失败')
      }
    },
    [refreshBoot, handleError],
  )

  const deleteList = useCallback(
    async (id: number) => {
      try {
        await api.deleteList(id)
        if (selection.kind === 'list' && selection.id === id) setSelection(DEFAULT_SELECTION)
        await refreshBoot()
        toast('清单已删除')
      } catch (e) {
        handleError(e, '删除清单失败')
      }
    },
    [selection, refreshBoot, toast, handleError],
  )

  const ensureTags = useCallback(
    async (names: string[]) => {
      try {
        const r = await api.ensureTags(names)
        setBoot((prev) => (prev ? { ...prev, tags: r.tags } : prev))
        return r.tags.filter((t) => names.includes(t.name))
      } catch (e) {
        handleError(e, '创建标签失败')
        return []
      }
    },
    [handleError],
  )

  const createTag = useCallback(
    async (name: string, color?: string) => {
      try {
        const t = await api.createTag({ name, color })
        await refreshBoot()
        return t
      } catch (e) {
        handleError(e, '创建标签失败')
        return null
      }
    },
    [refreshBoot, handleError],
  )

  const updateTag = useCallback(
    async (id: number, patch: { name?: string; color?: string }) => {
      try {
        await api.updateTag(id, patch)
        await refreshBoot()
        void refreshTasks()
      } catch (e) {
        handleError(e, '更新标签失败')
      }
    },
    [refreshBoot, refreshTasks, handleError],
  )

  const deleteTag = useCallback(
    async (id: number) => {
      try {
        await api.deleteTag(id)
        if (selection.kind === 'tag' && selection.id === id) setSelection(DEFAULT_SELECTION)
        await refreshBoot()
        void refreshTasks()
      } catch (e) {
        handleError(e, '删除标签失败')
      }
    },
    [selection, refreshBoot, refreshTasks, handleError],
  )

  // ---------- 今日三件事 ----------

  const todayFocusIds = useMemo(() => {
    const raw = settings.dailyFocus
    if (!raw) return []
    try {
      const parsed = JSON.parse(raw) as DailyFocus
      return parsed.date === todayStr() ? parsed.ids : []
    } catch {
      return []
    }
  }, [settings.dailyFocus])

  const setTodayFocus = useCallback(
    async (ids: number[]) => {
      await saveSettings({ dailyFocus: JSON.stringify({ date: todayStr(), ids }) })
    },
    [saveSettings],
  )

  // ---------- 提醒处理 ----------

  const dismissReminder = useCallback((key: string) => {
    setReminders((prev) => prev.filter((h) => `${h.task.id}|${h.fireAt}` !== key))
  }, [])

  const snoozeReminder = useCallback(
    (hit: ReminderHit, minutes: number) => {
      const key = `${hit.task.id}|${hit.fireAt}`
      setReminders((prev) => prev.filter((h) => `${h.task.id}|${h.fireAt}` !== key))
      const t = window.setTimeout(() => {
        setReminders((prev) => [...prev, hit])
        pushNotification(`慎始 · ${hit.task.title}`, '稍后提醒')
        playChime()
      }, minutes * 60_000)
      snoozeTimers.current.push(t)
      toast(`${minutes} 分钟后再提醒`)
    },
    [toast],
  )

  useEffect(() => {
    const timers = snoozeTimers.current
    return () => timers.forEach((t) => window.clearTimeout(t))
  }, [])

  // ---------- 专注 ----------

  const startFocus = useCallback((taskId: number | null, minutes: number) => {
    setFocus({
      taskId,
      minutes,
      running: true,
      endsAt: Date.now() + minutes * 60_000,
      remaining: minutes * 60,
    })
  }, [])

  const stopFocus = useCallback(
    async (completed: boolean) => {
      const elapsed = focus.minutes * 60 - focus.remaining
      setFocus({ taskId: null, minutes: 25, running: false, endsAt: null, remaining: 25 * 60 })
      if (completed && elapsed >= 60) {
        try {
          await api.addFocus({ taskId: focus.taskId, minutes: Math.round(elapsed / 60) })
          toast(`专注 ${Math.round(elapsed / 60)} 分钟已记录`)
          void loadStats()
        } catch (e) {
          handleError(e, '记录专注失败')
        }
      }
    },
    [focus.minutes, focus.remaining, focus.taskId, toast, loadStats, handleError],
  )
  focusFinishRef.current = stopFocus

  // ---------- 确认框 ----------

  const confirm = useCallback(
    (opts: { title: string; message: string; confirmText?: string; danger?: boolean }) =>
      new Promise<boolean>((resolve) => {
        setConfirmState({
          id: ++confirmSeq.current,
          title: opts.title,
          message: opts.message,
          confirmText: opts.confirmText ?? '确定',
          danger: opts.danger ?? false,
          resolve,
        })
      }),
    [],
  )

  const resolveConfirm = useCallback(
    (ok: boolean) => {
      setConfirmState((prev) => {
        prev?.resolve(ok)
        return null
      })
    },
    [],
  )

  const select = useCallback((s: Selection) => {
    setSelection(s)
    setKeywordState('')
    setSelectedTaskId(null)
    setSelectedIds([])
    setMultiSelect(false)
  }, [])

  const selectSmart = useCallback((key: SmartKey) => select({ kind: 'smart', key }), [select])

  // 进入搜索前记下原选择：清空关键词后要回到原来的清单，而不是停留在「搜索」这个伪清单上。
  const preSearch = useRef<Selection>(DEFAULT_SELECTION)

  const setKeyword = useCallback((k: string) => {
    setKeywordState(k)
    if (k.trim()) {
      setSelection((prev) => {
        if (prev.kind !== 'search') preSearch.current = prev
        return { kind: 'search', q: k.trim() }
      })
      return
    }
    setSelection(preSearch.current)
  }, [])

  const toggleSelected = useCallback((id: number) => {
    setSelectedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }, [])

  const clearSelected = useCallback(() => setSelectedIds([]), [])

  const setFilters = useCallback((f: TaskFilter) => setFiltersState(f), [])
  const resetFilters = useCallback(() => setFiltersState(EMPTY_FILTER), [])

  const value: StoreShape = {
    loading,
    boot,
    tasks,
    tasksLoading,
    selection,
    view,
    keyword,
    filters,
    settings,
    toasts,
    reminders,
    stats,
    repeatMeta,
    selectedTaskId,
    selectedIds,
    multiSelect,
    focus,
    confirmState,
    folders: boot?.folders ?? [],
    lists: boot?.lists ?? [],
    tags: boot?.tags ?? [],
    counts: boot?.counts ?? {},
    todayFocusIds,
    savedFilters,
    undo,
    activities,
    activitiesLoaded,
    version,

    select,
    selectSmart,
    setView,
    setKeyword,
    setFilters,
    resetFilters,
    setSelectedTask: setSelectedTaskId,
    setMultiSelect,
    toggleSelected,
    clearSelected,

    refreshTasks,
    refreshBoot,
    reconcile,

    createTask,
    updateTask,
    deleteTask,
    toggleTask,
    moveTask,
    skipTask,
    duplicateTask,
    batch,
    purgeCompleted,
    reorderTasks,

    undoDelete,
    dropUndo,

    loadActivities,
    clearActivities,

    saveFilter,
    applySavedFilter,
    renameSavedFilter,
    deleteSavedFilter,

    locked,
    unlock,

    addSubtask,
    updateSubtask,
    deleteSubtask,

    createFolder,
    updateFolder,
    deleteFolder,
    reorderFolders,
    createList,
    updateList,
    deleteList,
    reorderLists,

    ensureTags,
    createTag,
    updateTag,
    deleteTag,

    saveSettings,
    sortBy,
    setSortBy,
    toast,
    dismissToast,

    dismissReminder,
    snoozeReminder,

    loadStats,
    setTodayFocus,

    startFocus,
    stopFocus,
    tickFocus,

    confirm,
    resolveConfirm,
  }

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
