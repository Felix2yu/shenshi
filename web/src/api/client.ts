/** 极简 API 客户端：只做 fetch 的包装、错误规整与类型标注。 */

import type {
  Activity,
  Attachment,
  BackupStatus,
  BatchAction,
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
  SavedFilter,
  Stats,
  Tag,
  Task,
  TaskLink,
  TaskPatch,
  TaskTemplate,
  TemplatePatch,
  ToggleResult,
  UndoState,
  Webhook,
  WebhookDelivery,
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
  /** archived=1 只看已归档任务（侧栏恢复区）；缺省为默认口径（排除已归档）。 */
  archived?: '0' | '1'
  sortBy?: TaskSort
  limit?: number
}

/** 列表排序方式。smart 让到期日与优先级主导；manual 才由拖拽出的 sort_order 决定。 */
export type TaskSort = 'smart' | 'manual' | 'priority' | 'due' | 'created' | 'updated' | 'completed' | 'title'

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
  /** 随备份一起恢复出来的附件数。 */
  attachments?: number
  /** 备份里有记录、但这次没有带来文件的附件数（裸 JSON 导入时常见）。 */
  attachmentsMissed?: number
}

/** 导出接口的下载地址，直接交给浏览器以附件形式下载。 */
export const EXPORT_URLS = {
  zip: '/api/export/zip',
  json: '/api/export',
  csv: '/api/export/csv',
} as const

export const api = {
  bootstrap: () => request<Bootstrap>('GET', '/api/bootstrap'),

  listTasks: (query: TaskQuery) =>
    request<{ tasks: Task[]; count: number }>('GET', `/api/tasks${qs(query as Record<string, string | number>)}`),

  getTask: (id: number) => request<Task>('GET', `/api/tasks/${id}`),
  /** 依赖阻塞状态：blocked_by 且对端未完成时给出阻塞者清单。 */
  taskBlocked: (id: number) =>
    request<{ blocked: boolean; blockers: Task[] }>('GET', `/api/tasks/${id}/blocked`),
  createTask: (patch: TaskPatch) => request<Task>('POST', '/api/tasks', patch),
  updateTask: (id: number, patch: TaskPatch) => request<Task>('PATCH', `/api/tasks/${id}`, patch),
  deleteTask: (id: number) => request<{ ok: boolean }>('DELETE', `/api/tasks/${id}`),
  toggleTask: (id: number) => request<ToggleResult>('POST', `/api/tasks/${id}/toggle`),
  /** 跳过重复任务的本次发生：只推进到下一次，不记为完成。 */
  skipTask: (id: number) => request<Task>('POST', `/api/tasks/${id}/skip`),
  moveTask: (id: number, body: { listId?: number; dueDate?: string | null; dueTime?: string | null }) =>
    request<Task>('POST', `/api/tasks/${id}/move`, body),
  /** 复制任务：结构照搬，状态归零。 */
  duplicateTask: (id: number) => request<Task>('POST', `/api/tasks/${id}/duplicate`),
  batch: (
    ids: number[],
    action: BatchAction,
    extra?: { listId?: number; dueDate?: string },
  ) => request<{ ok: boolean; affected: number }>('POST', '/api/tasks/batch', { ids, action, ...extra }),
  /** 清空已完成任务；传 listId 表示只清该清单内的。 */
  purgeCompleted: (listId?: number) =>
    request<{ ok: boolean; affected: number }>('POST', '/api/tasks/purge', listId ? { listId } : {}),
  reorderTasks: (ids: number[]) => request<{ ok: boolean; count: number }>('POST', '/api/tasks/reorder', { ids }),

  // ---- 撤销与操作历史 ----
  /** 最近一次删除是否还可以挽回。 */
  undoState: () => request<UndoState>('GET', '/api/undo'),
  /** 恢复最近一次删除掉的任务。 */
  undo: () => request<{ ok: boolean; restored: number }>('POST', '/api/undo'),
  /** 放弃撤销机会（附件此刻才真正从磁盘删除）。 */
  dropUndo: () => request<{ ok: boolean }>('DELETE', '/api/undo'),
  listActivities: (limit = 120) =>
    request<{ activities: Activity[] }>('GET', `/api/activities?limit=${limit}`),
  clearActivities: () => request<{ ok: boolean }>('DELETE', '/api/activities'),

  // ---- 保存的筛选条件 ----
  listSavedFilters: () => request<{ savedFilters: SavedFilter[] }>('GET', '/api/saved-filters'),
  createSavedFilter: (body: { name: string; query: string; sortOrder?: number }) =>
    request<SavedFilter>('POST', '/api/saved-filters', body),
  updateSavedFilter: (id: number, body: Partial<{ name: string; query: string; sortOrder: number }>) =>
    request<{ ok: boolean }>('PATCH', `/api/saved-filters/${id}`, body),
  deleteSavedFilter: (id: number) => request<{ ok: boolean }>('DELETE', `/api/saved-filters/${id}`),

  addSubtask: (taskId: number, title: string, opts: { parentId?: number; dueDate?: string | null } = {}) =>
    request<{ id: number }>('POST', `/api/tasks/${taskId}/subtasks`, { title, ...opts }),
  updateSubtask: (id: number, patch: { title?: string; done?: boolean; sortOrder?: number; dueDate?: string | null; reminders?: number[] }) =>
    request<{ ok: boolean }>('PATCH', `/api/subtasks/${id}`, patch),
  deleteSubtask: (id: number) => request<{ ok: boolean }>('DELETE', `/api/subtasks/${id}`),

  // ---- 任务间关联 / 依赖 ----
  addTaskLink: (taskId: number, linkedTaskId: number, kind: 'related' | 'blocked_by') =>
    request<TaskLink>('POST', `/api/tasks/${taskId}/links`, { linkedTaskId, kind }),
  deleteTaskLink: (id: number) => request<{ ok: boolean }>('DELETE', `/api/task-links/${id}`),

  listFolders: () => request<{ folders: Folder[] }>('GET', '/api/folders'),
  createFolder: (body: { name: string; color?: string; icon?: string; parentId?: number }) =>
    request<Folder>('POST', '/api/folders', body),
  updateFolder: (
    id: number,
    body: Partial<{
      name: string
      color: string
      icon: string
      sortOrder: number
      collapsed: boolean
      archived: boolean
      parentId: number | null
      moveToRoot: boolean
    }>,
  ) => request<{ ok: boolean }>('PATCH', `/api/folders/${id}`, body),
  deleteFolder: (id: number) => request<{ ok: boolean }>('DELETE', `/api/folders/${id}`),
  reorderFolders: (ids: number[]) => request<{ ok: boolean; count: number }>('PUT', '/api/folders/reorder', { ids }),

  listLists: () => request<{ lists: List[] }>('GET', '/api/lists'),
  createList: (body: { name: string; color?: string; icon?: string; folderId?: number }) =>
    request<List>('POST', '/api/lists', body),
  updateList: (
    id: number,
    body: Partial<{
      name: string
      color: string
      icon: string
      sortOrder: number
      folderId: number
      moveToRoot: boolean
      archived: boolean
      starred: boolean
    }>,
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

  /** 习惯看板：习惯 + 区间打卡流水 + 统计一次取回，默认区间为最近 12 周；includeArchived=1 连同已归档习惯一并返回。 */
  listHabits: (query: { from?: string; to?: string; includeArchived?: '1' } = {}) =>
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
  /**
   * 上传备份文件导入：可以是裸 JSON，也可以是导出接口产出的压缩包（含附件）。
   * 走 multipart 而不是塞进 JSON —— 附件是字节流，base64 会白白膨胀三分之一。
   */
  importBackupFile: async (file: File, mode: 'merge' | 'replace') => {
    const fd = new FormData()
    fd.append('file', file)
    const resp = await fetch(`/api/import/file?mode=${mode}`, { method: 'POST', body: fd })
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
    return data as ImportResult
  },

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
  /** 稍后提醒：服务端记录推迟窗口，到期后经轮询重新投递（刷新/跨标签页都不丢）。 */
  snoozeReminder: (taskId: number, fireAt: string, minutes: number) =>
    request<{ ok: boolean; until: string }>('POST', '/api/reminders/snooze', { taskId, fireAt, minutes }),
  resetReminders: (taskId?: number) =>
    request<{ ok: boolean }>('POST', '/api/reminders/reset', taskId ? { taskId } : {}),

  repeatMeta: () => request<RepeatMeta>('GET', '/api/meta/repeat'),

  // ---- 附件 ----
  listAttachments: (taskId: number) => request<Attachment[]>('GET', `/api/tasks/${taskId}/attachments`),
  /** 上传走 multipart，不能复用 JSON 的 request 封装。 */
  uploadAttachment: async (taskId: number, file: File): Promise<Attachment> => {
    const form = new FormData()
    form.append('file', file)
    const resp = await fetch(`/api/tasks/${taskId}/attachments`, { method: 'POST', body: form })
    const text = await resp.text()
    const data = text ? JSON.parse(text) : null
    if (!resp.ok) {
      if (resp.status === 401) onUnauthorized?.()
      throw new ApiError(data?.error || `上传失败（${resp.status}）`, resp.status)
    }
    return data as Attachment
  },
  /** 附件下载地址：交给浏览器直接下载，便于大文件走流式。 */
  attachmentURL: (id: number) => `/api/attachments/${id}`,
  deleteAttachment: (id: number) => request<{ ok: boolean; id: number }>('DELETE', `/api/attachments/${id}`),

  // ---- 出站 Webhook ----
  listWebhooks: () => request<Webhook[]>('GET', '/api/webhooks'),
  createWebhook: (body: { name?: string; url: string; secret?: string; events?: string[]; enabled?: boolean }) =>
    request<Webhook>('POST', '/api/webhooks', body),
  updateWebhook: (
    id: number,
    body: Partial<{ name: string; url: string; secret: string; events: string[]; enabled: boolean }>,
  ) => request<Webhook>('PATCH', `/api/webhooks/${id}`, body),
  deleteWebhook: (id: number) => request<{ ok: boolean }>('DELETE', `/api/webhooks/${id}`),
  testWebhook: (id: number) => request<{ ok: boolean }>('POST', `/api/webhooks/${id}/test`),
  listDeliveries: (id: number) => request<WebhookDelivery[]>('GET', `/api/webhooks/${id}/deliveries`),

  // ---- 模板任务 ----
  listTemplates: () => request<TaskTemplate[]>('GET', '/api/templates'),
  createTemplate: (patch: TemplatePatch) => request<TaskTemplate>('POST', '/api/templates', patch),
  updateTemplate: (id: number, patch: TemplatePatch) => request<TaskTemplate>('PATCH', `/api/templates/${id}`, patch),
  deleteTemplate: (id: number) => request<{ ok: boolean }>('DELETE', `/api/templates/${id}`),
  instantiateTemplate: (id: number, body: { listId?: number; dueDate?: string } = {}) =>
    request<Task>('POST', `/api/templates/${id}/instantiate`, body),

  // ---- 自动备份 ----
  backupStatus: () => request<BackupStatus>('GET', '/api/backups'),
  runBackup: (keep?: number) => request<{ ok: boolean; file: string }>('POST', `/api/backups/run${qs({ keep })}`),

  /** 鉴权状态：是否需要口令、当前是否已通过。未启用口令时 authenticated 为 false。 */
  authStatus: () => request<{ required: boolean; authenticated: boolean }>('GET', '/api/auth/status'),
  login: (token: string) => request<{ ok: boolean }>('POST', '/api/auth/login', { token }),
  logout: () => request<{ ok: boolean }>('POST', '/api/auth/logout'),
}
