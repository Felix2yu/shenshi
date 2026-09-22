/** 极简 API 客户端：只做 fetch 的包装、错误规整与类型标注。 */

import type {
  Bootstrap,
  FocusSession,
  Habit,
  HabitBoard,
  HabitLog,
  HabitPatch,
  List,
  Folder,
  ReminderHit,
  Review,
  RepeatMeta,
  Stats,
  Tag,
  Task,
  TaskPatch,
  ToggleResult,
} from '../types'

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * 401 的统一出口。服务端启用访问口令后，会话可能在页面开着的时候失效
 * （Cookie 过期、服务重启换口令），这时不该只在角上弹一条错误，而应直接请用户重新输入。
 */
let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    headers: { Accept: 'application/json' },
  }
  if (body !== undefined) {
    init.headers = { ...init.headers, 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  let resp: Response
  try {
    resp = await fetch(path, init)
  } catch {
    throw new ApiError('无法连接到服务，请确认「慎始」服务正在运行', 0)
  }
  const text = await resp.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { error: text }
    }
  }
  if (!resp.ok) {
    if (resp.status === 401) onUnauthorized?.()
    const msg = (data as { error?: string } | null)?.error || `请求失败（${resp.status}）`
    throw new ApiError(msg, resp.status)
  }
  return data as T
}

function qs(params: Record<string, string | number | undefined | null>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export interface TaskQuery {
  smart?: string
  listId?: number
  folderId?: number
  tagId?: number
  status?: 'todo' | 'done' | 'all'
  date?: string
  from?: string
  to?: string
  priority?: number
  quadrant?: string
  q?: string
  sortBy?: TaskSort
  limit?: number
}

/** 列表排序方式。smart 让到期日与优先级主导；manual 才由拖拽出的 sort_order 决定。 */
export type TaskSort = 'smart' | 'manual' | 'priority' | 'due' | 'created' | 'title'

/** 导入方式：merge 追加为副本，replace 清空后重建。 */
export type ImportMode = 'merge' | 'replace'

export interface ImportResult {
  mode: string
  folders: number
  lists: number
  tasks: number
  tags: number
  reviews: number
  focus: number
}

/** 导出接口的下载地址，直接交给浏览器以附件形式下载。 */
export const EXPORT_URLS = { json: '/api/export', csv: '/api/export/csv' } as const

export const api = {
  bootstrap: () => request<Bootstrap>('GET', '/api/bootstrap'),

  listTasks: (query: TaskQuery) =>
    request<{ tasks: Task[]; count: number }>('GET', `/api/tasks${qs(query as Record<string, string | number>)}`),

  getTask: (id: number) => request<Task>('GET', `/api/tasks/${id}`),
  createTask: (patch: TaskPatch) => request<Task>('POST', '/api/tasks', patch),
  updateTask: (id: number, patch: TaskPatch) => request<Task>('PATCH', `/api/tasks/${id}`, patch),
  deleteTask: (id: number) => request<{ ok: boolean }>('DELETE', `/api/tasks/${id}`),
  toggleTask: (id: number) => request<ToggleResult>('POST', `/api/tasks/${id}/toggle`),
  /** 跳过重复任务的本次发生：只推进到下一次，不记为完成。 */
  skipTask: (id: number) => request<Task>('POST', `/api/tasks/${id}/skip`),
  moveTask: (id: number, body: { listId?: number; dueDate?: string | null; dueTime?: string | null }) =>
    request<Task>('POST', `/api/tasks/${id}/move`, body),
  batch: (ids: number[], action: 'complete' | 'reopen' | 'delete' | 'move', extra?: { listId?: number; dueDate?: string }) =>
    request<{ ok: boolean; affected: number }>('POST', '/api/tasks/batch', { ids, action, ...extra }),
  reorderTasks: (ids: number[]) => request<{ ok: boolean; count: number }>('POST', '/api/tasks/reorder', { ids }),

  addSubtask: (taskId: number, title: string) =>
    request<{ id: number }>('POST', `/api/tasks/${taskId}/subtasks`, { title }),
  updateSubtask: (id: number, patch: { title?: string; done?: boolean; sortOrder?: number }) =>
    request<{ ok: boolean }>('PATCH', `/api/subtasks/${id}`, patch),
  deleteSubtask: (id: number) => request<{ ok: boolean }>('DELETE', `/api/subtasks/${id}`),

  listFolders: () => request<{ folders: Folder[] }>('GET', '/api/folders'),
  createFolder: (body: { name: string; color?: string; icon?: string }) =>
    request<Folder>('POST', '/api/folders', body),
  updateFolder: (id: number, body: Partial<{ name: string; color: string; icon: string; sortOrder: number; collapsed: boolean }>) =>
    request<{ ok: boolean }>('PATCH', `/api/folders/${id}`, body),
  deleteFolder: (id: number) => request<{ ok: boolean }>('DELETE', `/api/folders/${id}`),
  reorderFolders: (ids: number[]) => request<{ ok: boolean; count: number }>('PUT', '/api/folders/reorder', { ids }),

  listLists: () => request<{ lists: List[] }>('GET', '/api/lists'),
  createList: (body: { name: string; color?: string; icon?: string; folderId?: number }) =>
    request<List>('POST', '/api/lists', body),
  updateList: (
    id: number,
    body: Partial<{ name: string; color: string; icon: string; sortOrder: number; folderId: number; moveToRoot: boolean }>,
  ) => request<{ ok: boolean }>('PATCH', `/api/lists/${id}`, body),
  deleteList: (id: number) => request<{ ok: boolean }>('DELETE', `/api/lists/${id}`),
  reorderLists: (ids: number[]) => request<{ ok: boolean; count: number }>('PUT', '/api/lists/reorder', { ids }),

  listTags: () => request<{ tags: Tag[] }>('GET', '/api/tags'),
  createTag: (body: { name: string; color?: string }) => request<Tag>('POST', '/api/tags', body),
  ensureTags: (names: string[]) => request<{ ids: number[]; tags: Tag[] }>('POST', '/api/tags/ensure', { names }),
  updateTag: (id: number, body: { name?: string; color?: string }) =>
    request<{ ok: boolean }>('PATCH', `/api/tags/${id}`, body),
  deleteTag: (id: number) => request<{ ok: boolean }>('DELETE', `/api/tags/${id}`),

  getSettings: () => request<Record<string, string>>('GET', '/api/settings'),  saveSettings: (kv: Record<string, string>) =>
    request<Record<string, string>>('PUT', '/api/settings', kv),

  /** 习惯看板：习惯 + 区间打卡流水 + 统计一次取回，默认区间为最近 12 周。 */
  listHabits: (query: { from?: string; to?: string } = {}) =>
    request<HabitBoard>('GET', `/api/habits${qs(query)}`),
  createHabit: (patch: HabitPatch) => request<Habit>('POST', '/api/habits', patch),
  updateHabit: (id: number, patch: HabitPatch) => request<Habit>('PATCH', `/api/habits/${id}`, patch),
  deleteHabit: (id: number) => request<{ ok: boolean }>('DELETE', `/api/habits/${id}`),
  reorderHabits: (ids: number[]) => request<{ updated: number }>('PUT', '/api/habits/reorder', { ids }),
  /** 打卡；不传 count 表示加一次，传 0 即撤销当天记录。 */
  checkHabit: (id: number, body: { day?: string; count?: number; note?: string } = {}) =>
    request<{ log: HabitLog | null }>('POST', `/api/habits/${id}/check`, body),
  uncheckHabit: (id: number, day: string) =>
    request<{ day: string }>('DELETE', `/api/habits/${id}/check${qs({ day })}`),

  /** 导出走浏览器直接下载（见 EXPORT_URLS），这里只提供导入。 */
  importBackup: (bundle: unknown, mode: 'merge' | 'replace') =>
    request<ImportResult>('POST', `/api/import?mode=${mode}`, bundle),

  stats: (days = 30) => request<Stats>('GET', `/api/stats?days=${days}`),
  listReviews: (limit = 30) => request<{ reviews: Review[] }>('GET', `/api/reviews?limit=${limit}`),
  getReview: (date: string) => request<Review>('GET', `/api/reviews/${date}`),
  saveReview: (body: { date: string; mood: string; wins: string; blockers: string; tomorrow: string }) =>
    request<Review>('PUT', '/api/reviews', body),

  listFocus: (limit = 20) => request<{ sessions: FocusSession[] }>('GET', `/api/focus?limit=${limit}`),
  addFocus: (body: { taskId?: number | null; minutes: number; startedAt?: string; endedAt?: string }) =>
    request<FocusSession>('POST', '/api/focus', body),

  dueReminders: (lookahead = 120) =>
    request<{ reminders: ReminderHit[]; serverTime: string }>('GET', `/api/reminders/due?lookahead=${lookahead}`),
  ackReminder: (taskId: number, fireAt: string) =>
    request<{ ok: boolean }>('POST', '/api/reminders/ack', { taskId, fireAt }),
  resetReminders: (taskId?: number) =>
    request<{ ok: boolean }>('POST', '/api/reminders/reset', taskId ? { taskId } : {}),

  repeatMeta: () => request<RepeatMeta>('GET', '/api/meta/repeat'),

  /** 鉴权状态：是否需要口令、当前是否已通过。未启用口令时 authenticated 为 false。 */
  authStatus: () => request<{ required: boolean; authenticated: boolean }>('GET', '/api/auth/status'),
  login: (token: string) => request<{ ok: boolean }>('POST', '/api/auth/login', { token }),
  logout: () => request<{ ok: boolean }>('POST', '/api/auth/logout'),
}
