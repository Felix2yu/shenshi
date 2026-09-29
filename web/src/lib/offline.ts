/**
 * 离线能力（页面侧）。
 *
 * 与 public/sw.js 的分工是固定的，别单边改：
 *   - SW 只管「读」：GET 网络优先、失败回退缓存；导航失败回退应用外壳。
 *   - 这里管「写」：离线时把操作排进 outbox，联网后逐条重放，再让上层对账。
 *     写这一半刻意放在页面而不是 SW —— 合成给前端看的响应体需要完整类型信息，
 *     而且 Safari 至今没有 Background Sync，SW 里的 sync 事件指望不上。
 *
 * 离线支持的动作（其余一律走在线路径，离线时报错提示需联网）：
 *   新建、编辑（标题/备注/优先级/日期等）、完成与恢复、改期改清单、删除。
 *
 * 两条硬约束：
 *   1. 重放必须幂等。队列里可能隔了几天，服务端状态也早被别的入口改过，
 *      所以完成走 POST /api/tasks/{id}/done（置位）而不是 /toggle（翻转）。
 *   2. 离线新建的任务用负数临时 id，联网重放成功后才换成服务端的真 id。
 *      队列里后续指向该任务的操作靠 idMap 改写，不依赖临时 id 一直有效。
 */

import { useCallback, useEffect, useRef, useState } from 'react'

import type { List, Selection, Subtask, Task, TaskPatch } from '../types'

// ---------- IndexedDB 薄封装 ----------

const DB_NAME = 'shenshi-offline'
const DB_VERSION = 1
const OUTBOX = 'outbox'
const LOCAL_TASKS = 'localTasks'

let dbPromise: Promise<IDBDatabase | null> | null = null

/**
 * 打开离线库。任何一步失败都降级成 null —— 那意味着「不提供离线写入」，
 * 应用其余部分照常工作，只是断网时写操作会照旧报错。Safari 无痕模式就会走这条路。
 */
function openDB(): Promise<IDBDatabase | null> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve) => {
    if (typeof indexedDB === 'undefined') {
      resolve(null)
      return
    }
    let opening: IDBOpenDBRequest
    try {
      opening = indexedDB.open(DB_NAME, DB_VERSION)
    } catch {
      resolve(null)
      return
    }
    opening.onupgradeneeded = () => {
      const db = opening.result
      if (!db.objectStoreNames.contains(OUTBOX)) {
        db.createObjectStore(OUTBOX, { keyPath: 'seq', autoIncrement: true })
      }
      if (!db.objectStoreNames.contains(LOCAL_TASKS)) {
        db.createObjectStore(LOCAL_TASKS, { keyPath: 'id' })
      }
    }
    opening.onsuccess = () => resolve(opening.result)
    opening.onerror = () => resolve(null)
    opening.onblocked = () => resolve(null)
  })
  return dbPromise
}

function wrap<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('IndexedDB 请求失败'))
  })
}

/** 写事务要等 oncomplete 才算落定：request 成功只代表入队，不代表已提交。 */
function commit(db: IDBDatabase, store: string, run: (s: IDBObjectStore) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const t = db.transaction(store, 'readwrite')
    t.oncomplete = () => resolve()
    t.onerror = () => reject(t.error ?? new Error('IndexedDB 事务失败'))
    t.onabort = () => reject(t.error ?? new Error('IndexedDB 事务中止'))
    try {
      run(t.objectStore(store))
    } catch (e) {
      reject(e instanceof Error ? e : new Error(String(e)))
    }
  })
}

async function readAll<T>(store: string): Promise<T[]> {
  const db = await openDB()
  if (!db) return []
  try {
    return (await wrap(db.transaction(store, 'readonly').objectStore(store).getAll())) as T[]
  } catch {
    return []
  }
}

// ---------- 队列模型 ----------

/** 离线时允许排队并重放的动作。 */
export type OutboxKind = 'create' | 'patch' | 'done' | 'delete' | 'move'

export interface OutboxItem {
  /** 自增主键，同时充当重放顺序。 */
  seq?: number
  kind: OutboxKind
  /** 服务端任务 id。create 时尚未存在，用临时负数占位（见 tempId）。 */
  taskId: number
  /** create / patch 的载荷。 */
  patch?: TaskPatch
  move?: { listId?: number; dueDate?: string | null; dueTime?: string | null }
  /** done 的目标状态；其他 kind 忽略。 */
  completed?: boolean
  /** create 专用：localTasks 里那条临时任务的 id。 */
  tempId?: number
  createdAt: number
  /** 重试次数。超过 MAX_TRIES 视为这条再也送不出去，直接丢弃。 */
  tries: number
  lastError?: string
}

/** 离线新建的任务用负数 id：与服务端的正数 id 天然不撞。 */
export function isTempTaskId(id: number): boolean {
  return id < 0
}

/**
 * 离线新建、尚未同步的任务。
 *
 * 多带一个 origin：这类任务还没落到服务端，视图归属只能由「它是在哪儿建的」决定。
 * 记下创建时的选择，比在各处重新推断「它算不算今天的事」可靠得多——
 * 推断规则一旦和服务端对不上，任务就会在用户眼前凭空消失，那是最难查的一类问题。
 */
export interface LocalTask extends Task {
  origin: { kind: Selection['kind']; key?: string; id?: number }
}

/** 这条离线任务是否属于当前选择的视图。 */
export function localTaskMatches(local: LocalTask, selection: Selection): boolean {
  if (local.origin.kind !== selection.kind) return false
  if (selection.kind === 'smart') return local.origin.key === selection.key
  // 搜索视图无从比较——搜索词本身还会变。一律显示：
  // 「明明记下了却看不见」比多显示一条更糟。
  if (selection.kind === 'search') return true
  return local.origin.id === selection.id
}

/**
 * 凭空造出一条「看起来像服务端返回的」任务。
 *
 * 服务端返回的 Task 字段很多（listName / listColor / folderId / 子任务进度…），
 * 少填一个，列表、看板、详情面板就会各有一处显示成空。这里按 Task 的完整形状补齐，
 * 代价是跟服务端模型要同步维护——但这是让离线新建的任务和在线新建的**长得一模一样**
 * 唯一稳妥的办法，不然用户会以为出了 bug。
 */
export function buildLocalTask(opts: {
  id: number
  patch: TaskPatch
  lists: Pick<List, 'id' | 'name' | 'color' | 'folderId'>[]
  inboxListId: number
  origin: LocalTask['origin']
}): LocalTask {
  const { id, patch, lists, inboxListId, origin } = opts
  const listId = patch.listId ?? inboxListId
  const list = lists.find((l) => l.id === listId)
  const now = new Date().toISOString()
  const drafts = patch.subtasks ?? []
  const subtasks = drafts.map((s, i): Subtask => {
    const subId = id * 1000 - (i + 1)
    return {
      id: subId,
      taskId: id,
      parentId: null,
      title: s.title,
      dueDate: null,
      reminders: [],
      done: s.done === true,
      sortOrder: s.sortOrder ?? i,
      children: [],
    }
  })
  return {
    id,
    listId,
    title: patch.title?.trim() || '未命名任务',
    notes: patch.notes ?? '',
    status: patch.status ?? 'todo',
    priority: patch.priority ?? 0,
    startDate: patch.startDate ?? null,
    dueDate: patch.dueDate ?? null,
    dueTime: patch.dueTime ?? null,
    endTime: patch.endTime ?? null,
    url: patch.url ?? '',
    reminders: patch.reminders ?? [],
    repeatRule: patch.repeatRule ?? null,
    repeatFrom: patch.repeatFrom ?? 'due',
    important: patch.important === true,
    urgent: patch.urgent === true,
    pinned: patch.pinned === true,
    starred: patch.starred === true,
    archived: patch.archived === true,
    estimateMinutes: 0,
    progress: 0,
    completedAt: null,
    sortOrder: 0,
    createdAt: now,
    updatedAt: now,
    subtasks,
    attachments: [],
    tags: [],
    links: [],
    listName: list?.name ?? '收集箱',
    listColor: list?.color ?? '',
    folderId: list?.folderId ?? null,
    subtaskDone: subtasks.filter((s) => s.done).length,
    subtaskOpen: subtasks.filter((s) => !s.done).length,
    origin,
  }
}

/** 离线创建的 taskId 在队列里没有服务端 id 可用，用临时 id 占位，重放时靠 idMap 改写。 */

export async function nextTempId(): Promise<number> {
  const existing = await readAll<LocalTask>(LOCAL_TASKS)
  const lowest = existing.reduce((m, t) => Math.min(m, t.id), 0)
  const id = lowest - 1
  // 极端情况下（同一次操作里连建两条）再退一格，宁可跳号也不要撞号。
  return existing.some((t) => t.id === id) ? id - 1 : id
}

export async function putLocalTask(task: LocalTask): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, LOCAL_TASKS, (s) => s.put(task)).catch(() => undefined)
}

export async function getLocalTasks(): Promise<LocalTask[]> {
  return readAll<LocalTask>(LOCAL_TASKS)
}

export async function dropLocalTask(id: number): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, LOCAL_TASKS, (s) => s.delete(id)).catch(() => undefined)
}

export async function clearLocalTasks(): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, LOCAL_TASKS, (s) => s.clear()).catch(() => undefined)
}

/**
 * 排一条待重放的操作。
 *
 * 只有「新建」会用到负数 taskId（此时它同时是 localTasks 里的临时 id）。
 * 其余动作一律针对服务端已有的任务、用正数 id —— 对临时任务的后续改动
 * 都就地改本地那一份，不进队列。改动这条约定前先看 retargetOutbox 的说明。
 */
export async function enqueue(item: Omit<OutboxItem, 'seq' | 'createdAt' | 'tries'>): Promise<void> {
  const db = await openDB()
  if (!db) return
  const full: OutboxItem = { ...item, createdAt: Date.now(), tries: 0 }
  await commit(db, OUTBOX, (s) => s.add(full)).catch(() => undefined)
}

export async function pendingCount(): Promise<number> {
  return (await readAll<OutboxItem>(OUTBOX)).length
}

async function removeFromOutbox(seq: number): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, OUTBOX, (s) => s.delete(seq)).catch(() => undefined)
}

async function updateOutboxItem(item: OutboxItem): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, OUTBOX, (s) => s.put(item)).catch(() => undefined)
}

/**
 * 把队列里后续指向某个临时 id 的操作改写成真实 id。
 *
 * 目前**用不到**——对临时任务的所有后续动作（编辑、完成、改期、删除）都是就地改
 * localTasks 里那一份，不入队，所以队列里不会出现「create 之后还有别的项指向它」。
 * 这里是给这个不变式上的护栏：万一将来给临时任务加了个入队的动作，
 * 没有这层改写就会静默丢数据——create 成功即出队，而临时 id → 真实 id 的映射
 * 只活在一轮重放的内存里，下一轮 patch 会打到 /api/tasks/-1 上去，必然 404，
 * 然后被当成「服务端明确拒绝」丢弃。届时只要这个函数在，就自动是对的。
 */
async function retargetOutbox(tempId: number, realId: number): Promise<void> {
  const db = await openDB()
  if (!db) return
  const rest = await readAll<OutboxItem>(OUTBOX)
  const affected = rest.filter((i) => i.taskId === tempId)
  if (!affected.length) return
  await commit(db, OUTBOX, (s) => {
    for (const item of affected) s.put({ ...item, taskId: realId })
  }).catch(() => undefined)
}

export async function clearOutbox(): Promise<void> {
  const db = await openDB()
  if (!db) return
  await commit(db, OUTBOX, (s) => s.clear()).catch(() => undefined)
}

// ---------- 网络状态 ----------

/**
 * 浏览器报的 online 只说明「有网卡」，不代表服务在跑。
 * 所以这里只把它当提示用，判定失败是否该排队看的是请求本身。
 */
export function useOnline(): boolean {
  const [online, setOnline] = useState(() => (typeof navigator === 'undefined' ? true : navigator.onLine))
  useEffect(() => {
    const up = () => setOnline(true)
    const down = () => setOnline(false)
    window.addEventListener('online', up)
    window.addEventListener('offline', down)
    return () => {
      window.removeEventListener('online', up)
      window.removeEventListener('offline', down)
    }
  }, [])
  return online
}

// ---------- 重放 ----------

/** 超过这个次数就不再重试：多半是这条数据本身有问题，再试也是一样的结果。 */
const MAX_TRIES = 8

export interface ReplayReport {
  /** 成功送达服务端的条数。 */
  replayed: number
  /** 离线新建任务真正落到服务端的条数。 */
  created: number
  /** 因服务端明确拒绝而丢弃的条数。 */
  dropped: number
  /** 中途因为仍然连不上而收手，剩下的留待下次。 */
  halted: boolean
}

let replaying = false

/** 重放进行中：让 client.ts 知道此时的失败不能再入队，否则会越积越多。 */
export function isReplaying(): boolean {
  return replaying
}

/**
 * 重放一条队列项。**直接用 fetch**，不走 api 客户端 ——
 * 走客户端会被排队层再拦一次，形成死循环。
 *
 * 返回值：ok 表示成功；retry 表示网络问题、应保留；drop 表示服务端拒绝、应丢弃。
 */
async function send(item: OutboxItem, realId: number): Promise<
  { ok: true; task?: Task } | { ok: false; retry: boolean; error: string }
> {
  const json = (body: unknown): RequestInit => ({
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  })
  const init = (method: string, body?: unknown): RequestInit => ({
    method,
    headers: {
      Accept: 'application/json',
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  })

  let url: string
  let options: RequestInit
  switch (item.kind) {
    case 'create':
      url = '/api/tasks'
      options = json(item.patch ?? {})
      break
    case 'patch':
      url = `/api/tasks/${realId}`
      options = init('PATCH', item.patch ?? {})
      break
    case 'done':
      url = `/api/tasks/${realId}/done`
      options = json({ completed: item.completed === true })
      break
    case 'delete':
      url = `/api/tasks/${realId}`
      options = init('DELETE')
      break
    case 'move':
      url = `/api/tasks/${realId}/move`
      options = json(item.move ?? {})
      break
  }

  try {
    const resp = await fetch(url, options)
    // 401 是会话失效，不是数据问题：留着队列，等用户重新解锁后再重放。
    if (resp.status === 401) return { ok: false, retry: true, error: '会话已失效' }
    if (resp.status >= 500) return { ok: false, retry: true, error: `服务异常（${resp.status}）` }
    if (!resp.ok) {
      let msg = `请求被拒绝（${resp.status}）`
      try {
        const body = (await resp.json()) as { error?: string }
        if (body?.error) msg = body.error
      } catch {
        /* 响应体不是 JSON，用状态码兜底 */
      }
      return { ok: false, retry: false, error: msg }
    }
    if (item.kind === 'create') {
      try {
        return { ok: true, task: (await resp.json()) as Task }
      } catch {
        /* 拿不到任务对象也不影响：下一条会走 idMap 之外的分支 */
        return { ok: true }
      }
    }
    return { ok: true }
  } catch {
    return { ok: false, retry: true, error: '网络不可用' }
  }
}

/**
 * 把 outbox 逐条重放到服务端。
 *
 * 顺序必须按 seq：离线期间「新建 A → 改 A 的日期 → 完成 A」这样一串操作，
 * 换序重放会得到完全不同（且大概率错误）的结果。
 * 指针的迁移靠 idMap：create 拿到真 id 后，后续指向临时 id 的操作改用真 id。
 */
export async function replayOutbox(): Promise<ReplayReport> {
  const report: ReplayReport = { replayed: 0, created: 0, dropped: 0, halted: false }
  if (replaying) return report

  const items = (await readAll<OutboxItem>(OUTBOX)).sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
  if (!items.length) return report

  replaying = true
  try {
    const idMap = new Map<number, number>()
    for (const item of items) {
      // 同一临时 id 在队列里可能出现多次（create 之后又 patch / done），
      // 换成真 id 之后再发，服务端看到的是同一个任务。
      const realId = idMap.get(item.taskId) ?? item.taskId

      const res = await send(item, realId)
      if (res.ok) {
        report.replayed += 1
        if (item.kind === 'create' && res.task) {
          report.created += 1
          if (item.tempId !== undefined) {
            idMap.set(item.taskId, res.task.id)
            // 临时任务已完成使命：真实的那条会被下一次对账拉回来。
            await dropLocalTask(item.tempId)
            // 队列里后续指向这条任务的操作要换成真 id。落盘改写而不是只记在内存里，
            // 是为了让映射活过「这一轮中途失败」的情况。
            await retargetOutbox(item.tempId, res.task.id)
          }
        }
        await removeFromOutbox(item.seq!)
        continue
      }

      if (!res.retry) {
        // 服务端明确不要了（任务早被删掉、清单没了之类）。
        // 记一笔、丢掉，否则这条会永远卡住后面的所有操作。
        report.dropped += 1
        if (item.tempId !== undefined) await dropLocalTask(item.tempId)
        await removeFromOutbox(item.seq!)
        continue
      }

      if (res.error === '会话已失效') {
        report.halted = true
        break
      }
      // 连不上：保留原样，下一轮再来。tries 只在真正失败时累加。
      const tries = item.tries + 1
      if (tries >= MAX_TRIES) {
        report.dropped += 1
        if (item.tempId !== undefined) await dropLocalTask(item.tempId)
        await removeFromOutbox(item.seq!)
        continue
      }
      await updateOutboxItem({ ...item, tries, lastError: res.error })
      report.halted = true
      break
    }
  } finally {
    replaying = false
  }
  return report
}

// ---------- Service Worker 注册与更新 ----------

export interface UpdateState {
  /** 已经有新版本装好了，正等着用户点「更新」。 */
  updateReady: boolean
  /** 点更新：让等待中的新 SW 立刻接管，然后重载。 */
  applyUpdate: () => Promise<void>
  /** 设置里的「彻底清除」：删光所有缓存与本地离线数据，注销 SW 后重载。 */
  hardReset: () => Promise<void>
  /** 当前是否由 Service Worker 接管（装在主屏幕上时为真）。 */
  controlled: boolean
}

function postToSW(message: unknown): Promise<void> {
  return navigator.serviceWorker?.ready
    .then((reg) => {
      reg.active?.postMessage(message)
    })
    .catch(() => undefined)
}

/**
 * 注册 SW 并盯住「有新版本」这件事。
 *
 * 关键在于**不自动 skipWaiting**：直接换掉正在用的页面，用户正在编辑的备注就没了。
 * 这里只把新版本标记成「可更新」，由用户点一下再切。
 */
export function useServiceWorker(): UpdateState {
  const [updateReady, setUpdateReady] = useState(false)
  const [controlled, setControlled] = useState(() => !!navigator.serviceWorker?.controller)
  const regRef = useRef<ServiceWorkerRegistration | null>(null)

  useEffect(() => {
    if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return
    // file:// 下没有 SW，也不该有。
    if (location.protocol !== 'https:' && location.hostname !== 'localhost' && location.hostname !== '127.0.0.1') {
      return
    }

    let disposed = false

    const track = (reg: ServiceWorkerRegistration) => {
      regRef.current = reg
      if (disposed) return
      // 已经有人在等更新了（上次没点「更新」就关了页面）。
      if (reg.waiting && navigator.serviceWorker.controller) setUpdateReady(true)

      reg.addEventListener('updatefound', () => {
        const installing = reg.installing
        if (!installing) return
        installing.addEventListener('statechange', () => {
          // controller 有值说明不是首次安装，而是「新版本替换旧版本」。
          if (installing.state === 'installed' && navigator.serviceWorker.controller) {
            setUpdateReady(true)
          }
        })
      })
    }

    navigator.serviceWorker
      .register('/sw.js', { scope: '/', updateViaCache: 'none' })
      .then((reg) => {
        if (disposed) return
        track(reg)
        // 每次打开都主动问一次有没有新版。Safari 对 SW 的检查相当保守，
        // 光靠浏览器的后台更新，用户往往隔很久都看不到新版本。
        reg.update().catch(() => undefined)
      })
      .catch(() => undefined)

    const onControllerChange = () => setControlled(true)
    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange)

    return () => {
      disposed = true
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange)
    }
  }, [])

  const applyUpdate = useCallback(async () => {
    const reg = regRef.current
    if (reg?.waiting) {
      await new Promise<void>((resolve) => {
        const onChange = () => {
          if (navigator.serviceWorker.controller) resolve()
        }
        navigator.serviceWorker.addEventListener('controllerchange', onChange, { once: true })
        reg.waiting?.postMessage({ type: 'SKIP_WAITING' })
        // 兜底：万一消息没送达，2 秒后照样重载，别把用户卡在旧版本上。
        setTimeout(() => resolve(), 2000)
      })
    }
    // 新 SW 接管后整页重载：旧 JS 已经把状态写进了内存，不重载会和新版本对不上。
    window.location.reload()
  }, [])

  const hardReset = useCallback(async () => {
    try {
      await postToSW({ type: 'CLEAR_CACHES' })
      await clearOutbox()
      await clearLocalTasks()
      const regs = await navigator.serviceWorker?.getRegistrations?.()
      await Promise.all((regs ?? []).map((r) => r.unregister()))
      if (typeof caches !== 'undefined') {
        const names = await caches.keys()
        await Promise.all(names.map((n) => caches.delete(n)))
      }
    } catch {
      /* 清不干净也要让页面重新开始，下面照常重载 */
    }
    window.location.reload()
  }, [])

  return { updateReady, applyUpdate, hardReset, controlled }
}
