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

import { ApiError, api, isNetworkError, setUnauthorizedHandler, type TaskQuery, type TaskSort } from '../api/client'
import { addDays, todayStr } from '../lib/date'
import { EMPTY_FILTER, filterFromQuery, filterToQuery, isFilterActive, type TaskFilter } from '../lib/filter'
import {
  buildLocalTask,
  dropLocalTask,
  enqueue,
  getLocalTasks,
  isTempTaskId,
  localTaskMatches,
  nextTempId,
  pendingCount,
  putLocalTask,
  replayOutbox,
  useOnline,
  useServiceWorker,
  type LocalTask,
} from '../lib/offline'
import { playChime, playTick, pushNotification } from '../lib/notify'
import {
  fontScaleOf,
  type Activity,
  type BatchAction,
  type Bootstrap,
  type DailyFocus,
  type Folder,
  type List,
  type ReminderHit,
  type RepeatMeta,
  type SavedFilter,
  type Selection,
  type Settings,
  type SmartKey,
  type Stats,
  type Tag,
  type Task,
  type TaskPatch,
  type ThemeMode,
  type UndoState,
  type ViewKind,
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
  /** 用户选择的明暗模式，含「自动」。 */
  themeMode: ThemeMode
  /** 实际生效的外观：把「自动」解析为跟随系统后的结果。 */
  resolvedTheme: 'light' | 'dark'
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
  /** 累计全量任务索引：每次拉取的任务都并入，跨视图保留。今日重点用它反查标题，
   *  避免焦点任务不在当前视图 tasks 子集里时 tasks.find 失败、误报「未选定今日重点」。 */
  taskIndex: Record<number, Task>
  savedFilters: SavedFilter[]
  /** 最近一次删除是否还能挽回；null 表示尚未问过服务端。 */
  undo: UndoState | null
  activities: Activity[]
  activitiesLoaded: boolean
  /** 每次写操作对账后自增。日历、统计等自持数据的视图据此重新拉取。 */
  version: number
  /** 统计接口最近一次加载是否失败。与「还没加载」区分开，避免把故障显示成空态。 */
  statsError: boolean
  /** 浏览器报的在线状态。只作提示用——它说有网卡，不代表服务在跑。 */
  online: boolean
  /** 离线队列里待同步的条数。 */
  pendingSync: number
  /** 联网后把队列重放一遍。返回是否真的送出去了（true 时界面已对账）。 */
  syncNow: () => Promise<boolean>
  /** 已经有装好的新版本，等用户点「更新」。 */
  updateReady: boolean
  /** 等待中这一版的标识；UI 用它记住「这一版已经关掉过了」。 */
  updateKey: string | null
  /** 切到新版本：让等待中的 SW 立刻接管，然后整页重载。 */
  applyUpdate: () => Promise<void>
  /** 设置里的「彻底清除」：删光缓存与离线数据、注销 SW 后重载。 */
  hardReset: () => Promise<void>
  /** 是否由 Service Worker 接管（装到主屏幕 / 程序坞后为真）。 */
  offlineReady: boolean

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

  addSubtask: (taskId: number, title: string, opts?: { parentId?: number; dueDate?: string | null }) => Promise<void>
  updateSubtask: (id: number, patch: { title?: string; done?: boolean; dueDate?: string | null; reminders?: number[] }) => Promise<void>
  deleteSubtask: (id: number) => Promise<void>
  /** 建立任务间关联（related / blocked_by）。 */
  addTaskLink: (taskId: number, linkedTaskId: number, kind: 'related' | 'blocked_by') => Promise<void>
  /** 删除一条任务间关联。 */
  removeTaskLink: (linkId: number) => Promise<void>

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
  ) => Promise<boolean>
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
  ) => Promise<boolean>
  deleteList: (id: number) => Promise<void>
  reorderLists: (ids: number[]) => Promise<void>

  ensureTags: (names: string[]) => Promise<Tag[] | null>
  createTag: (name: string, color?: string) => Promise<Tag | null>
  updateTag: (id: number, patch: { name?: string; color?: string }) => Promise<boolean>
  deleteTag: (id: number) => Promise<void>

  saveSettings: (patch: Settings) => Promise<void>
  sortBy: TaskSort
  setSortBy: (v: TaskSort) => void
  toast: (message: string, kind?: Toast['kind']) => void
  dismissToast: (id: number) => void

  dismissReminder: (key: string) => void
  snoozeReminder: (hit: ReminderHit, minutes: number) => Promise<void>

  loadStats: (days?: number) => Promise<void>
  setTodayFocus: (ids: number[]) => Promise<void>
  registerTasks: (ts: Task[]) => void

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

/** 两条本地任务表是否等价（按 id + 内容判重）。用于避免无谓的状态更新。 */
function sameLocalTasks(a: LocalTask[], b: LocalTask[]): boolean {
  if (a === b) return true
  if (a.length !== b.length) return false
  return a.every((t, i) => {
    const o = b[i]
    return o.id === t.id && o.updatedAt === t.updatedAt && o.title === t.title && o.status === t.status
  })
}

/**
 * 把离线新建、尚未同步的任务并进当前列表。
 *
 * 放在这里而不是让 SW 去合并：这类任务的视图归属由「它是在哪儿建的」决定（见
 * LocalTask.origin），只有页面知道当时的 selection。SW 拿不到这个上下文。
 * 排序按创建时间倒序放在最前——离线记下的事通常是刚发生、且正想再看一眼的。
 */
function mergeLocalTasks(base: Task[], locals: LocalTask[], selection: Selection, keyword: string): Task[] {
  if (!locals.length) return base
  const kw = keyword.trim()
  const extra = locals
    .filter((l) => localTaskMatches(l, selection))
    .filter((t) => (kw ? `${t.title}\n${t.notes}`.toLowerCase().includes(kw.toLowerCase()) : true))
    .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''))
  return extra.length ? [...extra, ...base] : base
}

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

/**
 * 分组树的重排。分组可嵌套，一次拖拽的 ids 可能属于任意一层，
 * 所以先在某一层里找：命中 ≥2 个就重排这一层，否则下沉到子分组继续找。
 */
function reorderFoldersInTree(tree: Folder[], ids: number[]): Folder[] {
  const pos = new Map(ids.map((id, i) => [id, i]))
  const slots: number[] = []
  tree.forEach((f, i) => {
    if (pos.has(f.id)) slots.push(i)
  })
  if (slots.length >= 2) {
    const picked = slots.map((i) => tree[i]).sort((a, b) => (pos.get(a.id) ?? 0) - (pos.get(b.id) ?? 0))
    const next = [...tree]
    slots.forEach((slot, k) => {
      next[slot] = picked[k]
    })
    return next
  }
  return tree.map((f) =>
    f.children && f.children.length ? { ...f, children: reorderFoldersInTree(f.children, ids) } : f,
  )
}

/**
 * 在分组树里定位一个节点并替换它。折叠、改名等单节点更新要走这里，
 * 否则只改到根级数组，子分组会「点了没反应」。
 */
function mapFolderInTree(tree: Folder[], id: number, fn: (f: Folder) => Folder): Folder[] {
  return tree.map((f) => {
    if (f.id === id) return fn(f)
    if (f.children && f.children.length) return { ...f, children: mapFolderInTree(f.children, id, fn) }
    return f
  })
}

/**
 * 「这条提醒已经弹过桌面通知」的记账集合。
 *
 * 必须落盘：只记在内存里的话，刷新一次页面就等于从头开始，服务端台账里那些
 * 「尚未处理」的提醒会被当成新提醒重新弹一遍——用户看到的是同一件事反复轰炸。
 * 键沿用去重口径 `ackId|fireAt`。
 */
const NOTIFIED_KEY = 'shenshi-reminders-notified'
/** 上限只是防止长年累月把 localStorage 撑大，超出按先进先出丢最早的。 */
const NOTIFIED_MAX = 400

/** 单例：Provider 全应用只有一个，没必要每个渲染都去读一次存储。 */
let notifiedKeys: Set<string> | null = null

function notified(): Set<string> {
  if (notifiedKeys) return notifiedKeys
  const s = new Set<string>()
  notifiedKeys = s
  try {
    const raw = localStorage.getItem(NOTIFIED_KEY)
    const parsed = raw ? (JSON.parse(raw) as unknown) : []
    if (Array.isArray(parsed)) for (const k of parsed) s.add(String(k))
  } catch {
    // 读不出来就当没弹过：多弹一次远比永久不再弹好。
  }
  return s
}

/** Set 的插入序就是先后序，从头截断即先进先出。 */
function rememberNotified(keys: string[]): void {
  if (!keys.length) return
  const s = notified()
  for (const k of keys) s.add(k)
  while (s.size > NOTIFIED_MAX) {
    const oldest = s.values().next().value
    if (oldest === undefined) break
    s.delete(oldest)
  }
  try {
    localStorage.setItem(NOTIFIED_KEY, JSON.stringify([...s]))
  } catch {
    /* 隐私模式或配额满：退化成本次页面内去重 */
  }
}

function forgetNotified(key: string): void {
  if (!notified().delete(key)) return
  try {
    localStorage.setItem(NOTIFIED_KEY, JSON.stringify([...notified()]))
  } catch {
    /* 同上 */
  }
}

/** 「彻底清除缓存并重载」连这份记账一起抹掉，否则清完之后 overdue 的老提醒不再提示。 */
function clearNotified(): void {
  notifiedKeys = null
  try {
    localStorage.removeItem(NOTIFIED_KEY)
  } catch {
    /* 清不掉也不该拦住彻底清除的其余步骤 */
  }
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [boot, setBoot] = useState<Bootstrap | null>(null)
  const [tasks, setTasks] = useState<Task[]>([])
  const [tasksLoading, setTasksLoading] = useState(false)
  // 累计全量任务索引：跨视图保留每个见过的任务，供今日重点反查标题，
  // 避免焦点任务不在当前视图 tasks 子集里时 tasks.find 失败、误报「未选定今日重点」。
  const [taskIndex, setTaskIndex] = useState<Record<number, Task>>({})
  useEffect(() => {
    setTaskIndex((prev) => {
      let changed = false
      const next = { ...prev }
      for (const t of tasks) {
        if (next[t.id] !== t) {
          next[t.id] = t
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [tasks])
  const [selection, setSelection] = useState<Selection>(DEFAULT_SELECTION)
  const [view, setView] = useState<ViewKind>('list')
  const [keyword, setKeywordState] = useState('')
  const [filters, setFiltersState] = useState<TaskFilter>(EMPTY_FILTER)
  const [settings, setSettings] = useState<Settings>({})
  // 系统外观偏好。「自动」档以它为准，并在运行期跟随系统切换（不用刷新页面）。
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false,
  )
  const themeMode: ThemeMode = settings.theme ?? 'auto'
  const resolvedTheme: 'light' | 'dark' = themeMode === 'auto' ? (systemDark ? 'dark' : 'light') : themeMode
  // 排序方式随设置持久化。默认「智能」，由到期日与优先级主导；
  // 只有切到「手动」时，用户拖拽出的 sort_order 才会真正决定顺序。
  const sortBy: TaskSort = (settings.sortBy as TaskSort) || 'smart'
  const [toasts, setToasts] = useState<Toast[]>([])
  const [reminders, setReminders] = useState<ReminderHit[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [statsError, setStatsError] = useState(false)
  const [repeatMeta, setRepeatMeta] = useState<RepeatMeta | null>(null)
  const [savedFilters, setSavedFilters] = useState<SavedFilter[]>([])
  // 离线相关：online 只作提示，pendingSync 是「还有多少件事没送到服务端」，
  // localTasks 是离线新建、尚未同步的任务（负数临时 id）。
  const online = useOnline()
  const [pendingSync, setPendingSync] = useState(0)
  const [localTasks, setLocalTasks] = useState<LocalTask[]>([])
  const { updateReady, updateKey, applyUpdate, hardReset: reloadWithoutSW, controlled } = useServiceWorker()
  // 彻底清除：SW、缓存、离线队列之外，桌面通知「弹过了」的记账也要抹掉，
  // 否则清完之后 overdue 的旧提醒再也提示不了。
  const hardReset = useCallback(async () => {
    clearNotified()
    await reloadWithoutSW()
  }, [reloadWithoutSW])
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
  /** 提醒的跨标签页广播通道，由下方 effect 装配/销毁。 */
  const remindChannel = useRef<BroadcastChannel | null>(null)

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
      // 「连不上」不等于「服务没启动」：有这套离线能力之后，断网也会走到这里。
      // 不说清楚是哪一种，用户会跑去重启一个其实好好跑着的服务。
      if (isNetworkError(e)) {
        toast(
          typeof navigator !== 'undefined' && !navigator.onLine
            ? '当前离线，该操作需要联网'
            : '服务没响应，请确认「慎始」正在运行',
          'error',
        )
        return
      }
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

  // 设置里的「显示已完成」接线到查询结果：关闭时日常视图隐藏已完成任务，
  // 「已完成 / 最近完成」这两个本来就是看完成记录的视图不受影响。
  const showCompleted = settings.showCompleted !== '0'

  // 列表请求序号：搜索/切换视图时会有多个请求并发在途，
  // 只允许"最新那次"写回结果，否则先发的慢响应会覆盖后发的快响应（搜索结果串台）。
  const tasksSeq = useRef(0)
  // 在途请求句柄：序号负责"结果不串台"，句柄负责"旧请求真的被取消"（省带宽也省服务端）。
  const tasksAbort = useRef<AbortController | null>(null)

  /**
   * 刷新待同步条数与本地任务表。
   *
   * 两者都只在离线动作之后才可能变，所以比对相同就保持原引用不动：
   * localTasks 一变，refreshTasks 的引用就变，而 refreshTasks 又被
   * 「视图 / 选择变化时拉取任务」的 effect 依赖 —— 每次都换新数组的话，
   * 每做一次写操作都会白白多打一次列表请求。
   */
  const refreshOfflineState = useCallback(async () => {
    const [pending, locals] = await Promise.all([pendingCount(), getLocalTasks()])
    setPendingSync(pending)
    setLocalTasks((prev) => (sameLocalTasks(prev, locals) ? prev : locals))
  }, [])

  const refreshTasks = useCallback(async () => {
    const seq = ++tasksSeq.current
    tasksAbort.current?.abort()
    const ctrl = new AbortController()
    tasksAbort.current = ctrl
    setTasksLoading(true)
    try {
      const r = await api.listTasks(queryFor(selection, keyword, sortBy), ctrl.signal)
      if (seq !== tasksSeq.current) return
      const doneView =
        selection.kind === 'smart' && (selection.key === 'done' || selection.key === 'recentdone')
      const base = !showCompleted && !doneView ? r.tasks.filter((t) => t.status !== 'done') : r.tasks
      // 离线时这个请求由 SW 回退到缓存，里面不含本机离线新建的那几条，补回来。
      setTasks(mergeLocalTasks(base, localTasks, selection, keyword))
    } catch (e) {
      if (seq !== tasksSeq.current) return
      // 被主动取消的请求不是错误：下一次请求已经在路上。
      if (ctrl.signal.aborted) return
      handleError(e, '加载任务失败')
    } finally {
      if (seq === tasksSeq.current) setTasksLoading(false)
    }
  }, [selection, keyword, sortBy, showCompleted, localTasks, handleError])

  /** 写操作后统一做一次去抖对账：刷新角标、必要时刷新列表。 */
  const reconcile = useCallback(() => {
    if (reconcileTimer.current) window.clearTimeout(reconcileTimer.current)
    reconcileTimer.current = window.setTimeout(() => {
      void refreshBoot()
      void refreshTasks()
      setVersion((v) => v + 1)
    }, 220)
  }, [refreshBoot, refreshTasks])

  /**
   * 把离线队列重放一遍。
   *
   * 刻意不依赖 Background Sync：Safari 至今没有 SyncManager，iOS/macOS 上
   * 「等浏览器叫醒你」这条路是不存在的。重放由页面自己驱动——
   * online 事件、切回前台、以及一道兜底定时，三条路都通向这里。
   */
  const syncNow = useCallback(async () => {
    if (typeof navigator !== 'undefined' && !navigator.onLine) return false
    const report = await replayOutbox()
    await refreshOfflineState()
    if (report.replayed > 0) {
      // 重放期间服务端可能已经被别处改动过（而且本机刚补进去几条），
      // 所以必须整体对账一次，不能只相信队列送出去的那些。
      reconcile()
    }
    if (report.dropped > 0) {
      toast(`有 ${report.dropped} 项离线操作已失效，已丢弃`, 'error')
    }
    if (report.replayed > 0 && report.created > 0) {
      toast(`已同步 ${report.replayed} 项离线操作`)
    }
    return report.replayed > 0
  }, [refreshOfflineState, reconcile, toast])

  // 队列条数放进 ref：调度用的定时器与事件不该因为这个数字变化而反复重建。
  const pendingRef = useRef(0)
  pendingRef.current = pendingSync

  useEffect(() => {
    void refreshOfflineState()
  }, [refreshOfflineState])

  // 联网后送队列。刚进来先等一拍，让首次加载的请求先落地，免得两边抢带宽。
  useEffect(() => {
    if (!online) return
    const t = window.setTimeout(() => void syncNow(), 1200)
    return () => window.clearTimeout(t)
  }, [online, syncNow])

  // 从后台切回前台时补一次。iOS 上从「已切走」的状态恢复，online 事件经常根本不触发。
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible' && navigator.onLine && pendingRef.current > 0) {
        void syncNow()
      }
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [syncNow])

  // 兜底：online 事件也未必每次都准。iOS 从后台切回、弱网反复切换时都常见
  // 「其实已经联网，却没有 online 事件」的情况，所以另有一道定期重试。
  // 只看 navigator.onLine 与积压量，不看 online 状态——状态是提示，浏览器才是事实。
  useEffect(() => {
    const t = window.setInterval(() => {
      if (pendingRef.current > 0 && navigator.onLine) void syncNow()
    }, 20_000)
    return () => window.clearInterval(t)
  }, [syncNow])

  const loadStats = useCallback(async (days = 30) => {
    try {
      setStats(await api.stats(days))
      setStatsError(false)
    } catch (e) {
      setStatsError(true)
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
        // 未设置过外观时默认「跟随系统」，避免第一次打开就与系统主题相逆。
        const stored = b.settings ?? {}
        const theme: ThemeMode =
          stored.theme === 'dark' || stored.theme === 'light' || stored.theme === 'auto'
            ? stored.theme
            : 'auto'
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

  // 系统外观变化时实时同步（macOS 的「自动」切换日落模式、定时切换都走这里）。
  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mq) return
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  // 主题与外观
  useEffect(() => {
    const root = document.documentElement
    root.dataset.theme = resolvedTheme
    root.dataset.accent = settings.accent || 'seal'
    root.style.colorScheme = resolvedTheme
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', resolvedTheme === 'dark' ? '#171513' : '#faf7f2')
  }, [resolvedTheme, settings.accent])

  // 界面字号：改根元素 font-size，正文与间距一起缩放。
  // 用百分比而不是 px，这样浏览器自身的「默认字号」偏好仍然生效。
  useEffect(() => {
    const root = document.documentElement
    const scale = fontScaleOf(settings.fontScale)
    if (scale === 1) root.style.removeProperty('font-size')
    else root.style.fontSize = `${Math.round(scale * 100)}%`
  }, [settings.fontScale])

  // 周期刷新角标，让「今天 / 最近7天」的计数不因跨天而失真
  useEffect(() => {
    const t = window.setInterval(() => void refreshBoot(), 5 * 60 * 1000)
    return () => window.clearInterval(t)
  }, [refreshBoot])

  // 提醒轮询：每 30 秒问一次服务端「现在该提醒什么」。
  // 口径：服务端集合为准全量对齐本地状态——改期/完成/别的标签页处理过的旧 hit
  // 自动消失，刷新后未处理的自动回来；「收到即回执」改成了「用户处理时才回执」，
  // 否则「稍后提醒」到期后服务端已永久静默，永远弹不回来。
  //
  // 桌面通知只对 fireAt 已到的那几条弹（服务端 lookahead 会提前两小时把提醒送进来，
  // 那是给提醒中心列「即将」用的，不是催办）。去重键落在 localStorage：
  // 只记在内存里，刷新一次就等于把未处理的提醒重新弹一遍。
  useEffect(() => {
    if (loading) return
    const poll = async () => {
      try {
        const r = await api.dueReminders(120)
        const hits = r.reminders
        // 去重与回执统一用 ackId（子任务为负数）：用 task.id 会让所有子任务提醒
        // 挤在「0|fireAt」一个键上互相过滤，且 ack 传 0 必被服务端拒绝。
        const due = hits.filter((h) => h.overdue)
        const fresh = due.filter((h) => !notified().has(`${h.ackId}|${h.fireAt}`))
        const freshKeys = fresh.map((h) => `${h.ackId}|${h.fireAt}`)
        rememberNotified(freshKeys)
        for (const h of fresh) {
          const name = h.subtask ? `${h.task.title} · ${h.subtask.title}` : h.task.title
          pushNotification(`慎始 · ${name}`, `${h.dueLabel}（已到时间）`)
        }
        if (fresh.length) {
          if (settings.soundOn !== '0') playChime()
          // 先到的标签页「认领」这些键，后到的不再重复弹通知。
          remindChannel.current?.postMessage({ type: 'seen', keys: freshKeys })
        }
        setReminders(hits)
      } catch {
        /* 轮询失败静默，下个周期再试 */
      }
    }
    void poll()
    const t = window.setInterval(() => void poll(), 30_000)
    return () => window.clearInterval(t)
  }, [loading, settings.soundOn])

  // 跨标签页同步：dismiss/snooze 一发生，其它标签页立刻跟进，
  // 不必等下一个 30 秒轮询周期。
  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') return
    const ch = new BroadcastChannel('shenshi-reminders')
    ch.onmessage = (ev: MessageEvent) => {
      const msg = ev.data as { type?: string; key?: string; keys?: string[] }
      if (!msg?.type) return
      const drop = (keys: string[]) => {
        const set = new Set(keys)
        setReminders((prev) => prev.filter((h) => !set.has(`${h.ackId}|${h.fireAt}`)))
      }
      if (msg.type === 'dismiss' && msg.key) {
        drop([msg.key])
        // 对方已落回执：本页也记下「弹过了」，防 stale 轮询把它带回来再弹一次。
        rememberNotified([msg.key])
      } else if (msg.type === 'snooze' && msg.key) {
        drop([msg.key])
        // 抹掉记号：推迟到期后应当重新弹一次。
        forgetNotified(msg.key)
      } else if (msg.type === 'seen' && msg.keys) {
        rememberNotified(msg.keys)
      }
    }
    remindChannel.current = ch
    return () => {
      remindChannel.current = null
      ch.close()
    }
  }, [])

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

  // 排序变化的重拉不在此单设 effect：sortBy 是 refreshTasks 的依赖，
  // 上方「视图 / 选择变化时拉取任务」的 effect 已随其引用变化触发 —— 再挂一条就是
  // 一次改排序打两个请求（09-24 审查 G14 的遗留，勿再加回）。

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

  /** 侧栏分组拖拽排序。ids 可以指向根级，也可以是某一层子分组。 */
  const reorderFolders = useCallback(
    async (ids: number[]) => {
      if (ids.length < 2) return
      setBoot((prev) => (prev ? { ...prev, folders: reorderFoldersInTree(prev.folders, ids) } : prev))
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

  /**
   * 断网时新建任务：造一条带临时 id 的本地任务存起来，同时把 create 排进队列。
   * 联网重放成功前它不占服务端 id，也就还不会被别的设备看到——这是有意为之。
   */
  const createTaskOffline = useCallback(
    async (patch: TaskPatch): Promise<Task | null> => {
      const tempId = await nextTempId()
      const local = buildLocalTask({
        id: tempId,
        patch,
        lists: boot?.lists ?? [],
        inboxListId: boot?.inboxListId ?? 0,
        origin:
          selection.kind === 'smart'
            ? { kind: 'smart', key: selection.key }
            : { kind: selection.kind, id: selection.kind === 'search' ? undefined : selection.id },
      })
      await putLocalTask(local)
      await enqueue({ kind: 'create', taskId: tempId, tempId, patch })
      await refreshOfflineState()
      setTasks((prev) => [local, ...prev])
      setTaskIndex((prev) => ({ ...prev, [tempId]: local }))
      toast(`已离线记下「${local.title}」· 联网后自动同步`)
      return local
    },
    [boot, selection, toast, refreshOfflineState],
  )

  const createTask = useCallback(
    async (patch: TaskPatch) => {
      try {
        // 新建任务的归属：要让任务**留在当前视图里**，否则用户看到的是一条 toast
        // 加一个空列表，第一反应是"没记上"。凡当前视图的约束能被表达的，就直接写进任务：
        // 标签视图补标签、逾期视图补过期日、高优先级补优先级、收藏补星标。
        if (patch.listId === undefined && selection.kind === 'list') patch.listId = selection.id
        if (selection.kind === 'tag' && patch.tagIds === undefined) patch.tagIds = [selection.id]
        if (patch.dueDate === undefined && patch.listId === undefined && selection.kind === 'smart') {
          // 智能清单：能靠日期/属性推断的补上（任务默认就是"今天做"的）
          if (selection.key === 'today') patch.dueDate = todayStr()
          else if (selection.key === 'tomorrow') patch.dueDate = addDays(todayStr(), 1)
          else if (selection.key === 'week' || selection.key === 'next7') patch.dueDate = todayStr()
          else if (selection.key === 'overdue') patch.dueDate = addDays(todayStr(), -1)
          else if (selection.key === 'high' && patch.priority === undefined) patch.priority = 3
          else if (selection.key === 'starred' && patch.starred === undefined) patch.starred = true
        }
        const t = await api.createTask(patch)
        // 分组 / 搜索等推断不出归属的视图如实说明它去了哪里（否则任务"即刻消失"）
        const home = selection.kind === 'list' ? '' : t.listName
        toast(home ? `已记下「${t.title}」· 存入「${home}」` : `已记下「${t.title}」`)
        reconcile()
        return t
      } catch (e) {
        if (isNetworkError(e)) return createTaskOffline(patch)
        handleError(e, '创建任务失败')
        return null
      }
    },
    [selection, toast, reconcile, handleError, createTaskOffline],
  )

  /**
   * 离线时改一条任务。
   *
   * 两条分支：改的是离线新建的任务（负数 id）就直接改本地那份，不必再排一条队列；
   * 改的是服务端已有的任务则既改本地（让界面立刻反映）又排一条 patch。
   */
  const updateTaskOffline = useCallback(
    async (id: number, patch: TaskPatch): Promise<Task | null> => {
      const isNew = isTempTaskId(id)
      if (isNew) {
        const current = localTasks.find((t) => t.id === id)
        if (!current) return null
        // subtasks 故意不并进来：TaskPatch 里那是「一串草稿」，
        // 而 Task.subtasks 是带 id / 父子关系的成品结构。离线这条路也不支持改子任务
        // （addSubtask 仍走在线接口），硬拼只会造出半成品。
        const { subtasks: _drafts, ...fields } = patch
        const next: LocalTask = { ...current, ...fields, id }
        if (patch.tagIds) {
          next.tags = patch.tagIds
            .map((tid) => (boot?.tags ?? []).find((t) => t.id === tid))
            .filter((t): t is Tag => !!t)
        }
        next.updatedAt = new Date().toISOString()
        await putLocalTask(next)
        await refreshOfflineState()
        setTasks((prev) => prev.map((t) => (t.id === id ? next : t)))
        return next
      }
      setTasks((prev) =>
        prev.map((t) => {
          if (t.id !== id) return t
          const { subtasks: _drafts, ...fields } = patch
          const merged: Task = { ...t, ...fields, id }
          if (patch.tagIds) {
            merged.tags = patch.tagIds
              .map((tid) => (boot?.tags ?? []).find((g) => g.id === tid))
              .filter((g): g is Tag => !!g)
          }
          if (patch.status === 'done') merged.completedAt = t.completedAt ?? new Date().toISOString()
          if (patch.status === 'todo' || patch.status === 'in_progress') merged.completedAt = null
          return merged
        }),
      )
      await enqueue({ kind: 'patch', taskId: id, patch })
      await refreshOfflineState()
      toast('改动已离线保存，联网后同步')
      return null
    },
    [localTasks, boot, toast, refreshOfflineState],
  )

  const updateTask = useCallback(
    async (id: number, patch: TaskPatch) => {
      try {
        const t = await api.updateTask(id, patch)
        patchLocalTask(t)
        reconcile()
        return t
      } catch (e) {
        if (isNetworkError(e)) return updateTaskOffline(id, patch)
        handleError(e, '更新任务失败')
        return null
      }
    },
    [patchLocalTask, reconcile, handleError],
  )

  /** 从界面各处移除一条任务：列表、索引、详情、多选——五处必须一起清。 */
  const removeTaskLocally = useCallback((id: number) => {
    setTasks((prev) => prev.filter((t) => t.id !== id))
    setTaskIndex((prev) => {
      if (!(id in prev)) return prev
      const n = { ...prev }
      delete n[id]
      return n
    })
    setSelectedIds((prev) => prev.filter((x) => x !== id))
  }, [])

  const deleteTask = useCallback(
    async (id: number) => {
      try {
        await api.deleteTask(id)
        removeTaskLocally(id)
        if (selectedTaskId === id) setSelectedTaskId(null)
        toast('已删除')
        reconcile()
      } catch (e) {
        if (isNetworkError(e)) {
          // 离线新建的任务还没落到服务端，删掉本地那份即可，不必排队列。
          if (isTempTaskId(id)) {
            await dropLocalTask(id)
            await refreshOfflineState()
            removeTaskLocally(id)
            if (selectedTaskId === id) setSelectedTaskId(null)
            toast('已删除')
            return
          }
          removeTaskLocally(id)
          if (selectedTaskId === id) setSelectedTaskId(null)
          await enqueue({ kind: 'delete', taskId: id })
          await refreshOfflineState()
          toast('已离线删除，联网后同步')
          return
        }
        handleError(e, '删除失败')
      }
    },
    [selectedTaskId, toast, reconcile, handleError, removeTaskLocally, refreshOfflineState],
  )

  /**
   * 离线完成 / 恢复。
   *
   * 排进队列的是「置位」而不是「翻转」：离线这段时间里服务端状态完全可能被别人
   * 改过，重放时再翻转一次就会得到与用户意图相反的结果。重复任务的顺延由服务端的
   * 幂等完成接口在重放那一刻补上，所以离线时先不预告「下一次安排在几号」。
   */
  const toggleTaskOffline = useCallback(
    async (id: number): Promise<{ task: Task; nextTask: Task | null } | null> => {
      const cur = tasks.find((t) => t.id === id)
      if (!cur) return null
      const completed = cur.status !== 'done'

      if (isTempTaskId(id)) {
        // 离线新建的任务：origin 是它的归属依据，改状态时不能丢。
        const next: LocalTask = {
          ...(cur as LocalTask),
          status: completed ? 'done' : 'todo',
          completedAt: completed ? new Date().toISOString() : null,
          updatedAt: new Date().toISOString(),
        }
        await putLocalTask(next)
        await refreshOfflineState()
        setTasks((prev) => prev.map((t) => (t.id === id ? next : t)))
        toast(completed ? '已完成 · 联网后同步' : '已恢复 · 联网后同步')
        return { task: next, nextTask: null }
      }

      setTasks((prev) =>
        prev.map((t) =>
          t.id === id
            ? {
                ...t,
                status: completed ? 'done' : 'todo',
                completedAt: completed ? new Date().toISOString() : null,
                updatedAt: new Date().toISOString(),
              }
            : t,
        ),
      )
      await enqueue({ kind: 'done', taskId: id, completed })
      await refreshOfflineState()
      if (completed && settings.soundOn !== '0') playTick()
      toast(completed ? '已完成 · 联网后同步' : '已恢复 · 联网后同步')
      return { task: { ...cur, status: completed ? 'done' : 'todo' }, nextTask: null }
    },
    [tasks, settings.soundOn, toast, refreshOfflineState],
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
        if (isNetworkError(e)) return toggleTaskOffline(id)
        handleError(e, '操作失败')
        void refreshTasks()
        return null
      }
    },
    [tasks, patchLocalTask, settings.soundOn, toast, reconcile, handleError, refreshTasks, toggleTaskOffline],
  )

  const moveTask = useCallback(
    async (id: number, body: { listId?: number; dueDate?: string | null; dueTime?: string | null }) => {
      try {
        const t = await api.moveTask(id, body)
        patchLocalTask(t)
        reconcile()
      } catch (e) {
        if (isNetworkError(e)) {
          setTasks((prev) =>
            prev.map((t) => {
              if (t.id !== id) return t
              const list = (boot?.lists ?? []).find((l) => l.id === (body.listId ?? t.listId))
              return {
                ...t,
                listId: body.listId ?? t.listId,
                listName: list?.name ?? t.listName,
                listColor: list?.color ?? t.listColor,
                folderId: list?.folderId ?? t.folderId,
                dueDate: body.dueDate === undefined ? t.dueDate : body.dueDate,
                dueTime: body.dueTime === undefined ? t.dueTime : body.dueTime,
              }
            }),
          )
          if (isTempTaskId(id)) {
            await updateTaskOffline(id, {
              listId: body.listId,
              dueDate: body.dueDate,
              dueTime: body.dueTime,
            })
          } else {
            await enqueue({ kind: 'move', taskId: id, move: body })
            await refreshOfflineState()
          }
          toast('已离线改期，联网后同步')
          return
        }
        handleError(e, '移动失败')
      }
    },
    [patchLocalTask, reconcile, handleError, boot, refreshOfflineState, updateTaskOffline, toast],
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
        // 「完成」是幂等的集合操作（已在服务端改为只对未完成项生效），
        // 文案要与实际动作对应，不能一律含糊成"已处理"。
        const label =
          action === 'complete'
            ? '已完成'
            : action === 'reopen'
              ? '已恢复'
              : action === 'delete'
                ? '已删除'
                : action === 'archive'
                  ? '已归档'
                  : action === 'unarchive'
                    ? '已取消归档'
                    : '已处理'
        toast(`${label} ${r.affected} 项`)
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

  /** 递归找「哪条任务的子树里含有这条子任务」——子任务可以嵌套，不能只扫顶层。 */
  const findTaskBySubtaskId = (list: Task[], id: number): Task | undefined =>
    list.find((t) => {
      const hit = (subs: Task['subtasks']): boolean => subs.some((s) => s.id === id || hit(s.children))
      return hit(t.subtasks)
    })

  const addSubtask = useCallback(
    async (taskId: number, title: string, opts?: { parentId?: number; dueDate?: string | null }) => {
      try {
        await api.addSubtask(taskId, title, opts)
        const t = await api.getTask(taskId)
        patchLocalTask(t)
      } catch (e) {
        handleError(e, '新增子任务失败')
      }
    },
    [patchLocalTask, handleError],
  )

  const updateSubtask = useCallback(
    async (id: number, patch: { title?: string; done?: boolean; dueDate?: string | null; reminders?: number[] }) => {
      try {
        await api.updateSubtask(id, patch)
        // 子任务归属父任务，重新取一次父任务即可拿到最新进度。
        const parent = findTaskBySubtaskId(tasks, id)
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
        const parent = findTaskBySubtaskId(tasks, id)
        if (parent) patchLocalTask(await api.getTask(parent.id))
      } catch (e) {
        handleError(e, '删除子任务失败')
      }
    },
    [tasks, patchLocalTask, handleError],
  )

  // ---------- 任务间关联 ----------

  const addTaskLink = useCallback(
    async (taskId: number, linkedTaskId: number, kind: 'related' | 'blocked_by') => {
      try {
        await api.addTaskLink(taskId, linkedTaskId, kind)
        patchLocalTask(await api.getTask(taskId))
      } catch (e) {
        handleError(e, '建立关联失败')
      }
    },
    [patchLocalTask, handleError],
  )

  const removeTaskLink = useCallback(
    async (linkId: number) => {
      // 删的是「视角」里的一条边；删完把当前任务刷新一遍即可。
      const owner = tasks.find((t) => t.links?.some((l) => l.id === linkId))
      try {
        await api.deleteTaskLink(linkId)
        if (owner) patchLocalTask(await api.getTask(owner.id))
      } catch (e) {
        handleError(e, '删除关联失败')
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
      // 折叠是高频交互，先本地生效再落库（之后不会 refreshBoot，这条路径是唯一来源）。
      if (patch.collapsed !== undefined) {
        setBoot((prev) =>
          prev
            ? {
                ...prev,
                folders: mapFolderInTree(prev.folders, id, (f) => ({ ...f, collapsed: patch.collapsed! })),
              }
            : prev,
        )
      }
      try {
        await api.updateFolder(id, patch)
        if (patch.collapsed === undefined) await refreshBoot()
        return true
      } catch (e) {
        handleError(e, '更新分组失败')
        await refreshBoot()
        return false
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
      // 收藏是单点交互，先本地翻转再落库；失败时把原值拨回去，不让 UI 说谎。
      // 用 holder 对象承载：赋值发生在 setBoot 回调里，直接 let 变量会被 TS 收窄成 null。
      const prev: { value: { starred: boolean; archived: boolean } | null } = { value: null }
      if (patch.starred !== undefined || patch.archived !== undefined) {
        setBoot((b) => {
          if (b) {
            const cur = b.lists.find((l) => l.id === id)
            if (cur) prev.value = { starred: cur.starred, archived: cur.archived }
          }
          return b
        })
        setBoot((prevBoot) =>
          prevBoot
            ? {
                ...prevBoot,
                lists: prevBoot.lists.map((l) =>
                  l.id === id
                    ? {
                        ...l,
                        starred: patch.starred ?? l.starred,
                        archived: patch.archived ?? l.archived,
                      }
                    : l,
                ),
              }
            : prevBoot,
        )
      }
      try {
        await api.updateList(id, patch)
        await refreshBoot()
        return true
      } catch (e) {
        handleError(e, '更新清单失败')
        if (prev.value) {
          const back = prev.value
          setBoot((prevBoot) =>
            prevBoot
              ? {
                  ...prevBoot,
                  lists: prevBoot.lists.map((l) =>
                    l.id === id ? { ...l, starred: back.starred, archived: back.archived } : l,
                  ),
                }
              : prevBoot,
          )
        }
        return false
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
        // null 表示「没建成」：调用方据此中止建任务，避免预览有标签、落库却丢了。
        return null
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
        return true
      } catch (e) {
        handleError(e, '更新标签失败')
        return false
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

  // ---------- 今日重点 ----------

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

  /** 把一批任务并入全量索引。晨省快照（overdue/today/inbox）里勾选的任务
   *  不经过主界面 tasks，需显式注册，今日重点才能显示真实标题。 */
  const registerTasks = useCallback((ts: Task[]) => {
    setTaskIndex((prev) => {
      let changed = false
      const next = { ...prev }
      for (const t of ts) {
        if (next[t.id] !== t) {
          next[t.id] = t
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [])

  // ---------- 提醒处理 ----------

  const dismissReminder = useCallback((key: string) => {
    setReminders((prev) => prev.filter((h) => `${h.ackId}|${h.fireAt}` !== key))
    // 回执推迟到「用户处理」这一刻：轮询收到即 ack 的旧口径会让 snooze 无处安放。
    const sep = key.indexOf('|')
    if (sep > 0) {
      const ackId = Number(key.slice(0, sep))
      const fireAt = key.slice(sep + 1)
      if (Number.isFinite(ackId) && ackId !== 0 && fireAt) {
        void api.ackReminder(ackId, fireAt).catch(() => undefined)
      }
    }
    remindChannel.current?.postMessage({ type: 'dismiss', key })
  }, [])

  const snoozeReminder = useCallback(
    async (hit: ReminderHit, minutes: number) => {
      const key = `${hit.ackId}|${hit.fireAt}`
      try {
        await api.snoozeReminder(hit.ackId, hit.fireAt, minutes)
      } catch (e) {
        // 服务端没记上就不能本地假装推迟：否则到期后轮询会把它默默带回来。
        handleError(e, '设置稍后提醒失败')
        return
      }
      setReminders((prev) => prev.filter((h) => `${h.ackId}|${h.fireAt}` !== key))
      // 到期重弹要重新通知：抹掉「弹过了」的记号，交给下一次轮询当新提醒。
      forgetNotified(key)
      remindChannel.current?.postMessage({ type: 'snooze', key })
      toast(`${minutes} 分钟后再提醒`)
    },
    [handleError, toast],
  )

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
    // 侧栏点击隐含「去看这个清单」：不切回列表视图的话，在看板/日历下点
    // 「今天」会毫无反馈（active 判定要求 view === 'list'），快捷键会切、鼠标却不会。
    setView('list')
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

  const clearSelected = useCallback(() => {
    // 清选择与退多选必须成对：只清 selectedIds 会把界面卡在多选模式，
    // Esc 按下毫无反应（与 select()/batch() 的成对先例口径一致）。
    setSelectedIds([])
    setMultiSelect(false)
  }, [])

  const setFilters = useCallback((f: TaskFilter) => setFiltersState(f), [])
  const resetFilters = useCallback(() => setFiltersState(EMPTY_FILTER), [])

  // value 必须 memo：Provider 因任意一次 setState 重渲染时，未 memo 的新对象会
  // 让全部 useStore 消费者（侧栏 / 工具栏 / 详情 / 各视图）无谓地整棵重渲染。
  // 依赖 = 下方对象引用到的每一个变量，改动对象成员时**必须**同步补依赖。
  const value: StoreShape = useMemo(
    () => ({
      loading,
      boot,
      tasks,
      tasksLoading,
      selection,
      view,
      keyword,
      filters,
      settings,
      themeMode,
      resolvedTheme,
      toasts,
      reminders,
      stats,
      statsError,
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
      taskIndex,
      savedFilters,
      undo,
      activities,
      activitiesLoaded,
      version,
      online,
      pendingSync,
      syncNow,
      updateReady,
      updateKey,
      applyUpdate,
      hardReset,
      offlineReady: controlled,

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
      addTaskLink,
      removeTaskLink,

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
      registerTasks,

      startFocus,
      stopFocus,
      tickFocus,

      confirm,
      resolveConfirm,
    }),
    [
    loading,
    boot,
    tasks,
    tasksLoading,
    selection,
    view,
    keyword,
    filters,
    settings,
    themeMode,
    resolvedTheme,
    toasts,
    reminders,
    stats,
    statsError,
    repeatMeta,
    selectedTaskId,
    selectedIds,
    multiSelect,
    focus,
    confirmState,
    todayFocusIds,
    taskIndex,
    savedFilters,
    undo,
    activities,
    activitiesLoaded,
    version,
    online,
    pendingSync,
    syncNow,
    updateReady,
    updateKey,
    applyUpdate,
    hardReset,
    controlled,
    select,
    selectSmart,
    setView,
    setKeyword,
    setFilters,
    resetFilters,
    setSelectedTaskId,
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
    addTaskLink,
    removeTaskLink,
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
    registerTasks,
    startFocus,
    stopFocus,
    tickFocus,
    confirm,
    resolveConfirm
    ],
  )

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
