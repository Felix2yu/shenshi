/** 与 Go 端 model 包一一对应的类型定义。 */

export type Priority = 0 | 1 | 2 | 3

export const PRIORITY_LABEL: Record<Priority, string> = {
  0: '无',
  1: '低',
  2: '中',
  3: '高',
}

export interface Subtask {
  id: number
  taskId: number
  title: string
  done: boolean
  sortOrder: number
}

export interface Tag {
  id: number
  name: string
  color: string
  createdAt?: string
  taskCount?: number
}

export interface Task {
  id: number
  listId: number
  title: string
  notes: string
  status: 'todo' | 'done'
  priority: Priority
  /** 计划开始日 YYYY-MM-DD */
  startDate: string | null
  dueDate: string | null
  dueTime: string | null
  endTime: string | null
  /** 关联链接（会议、文档、单号），与附件分开存放 */
  url: string
  reminders: number[]
  repeatRule: string | null
  /** 重复任务的续期基准：due=从原到期日推，done=从实际完成日推 */
  repeatFrom: 'due' | 'done'
  important: boolean
  urgent: boolean
  /** 置顶：始终排在未完成列表最前 */
  pinned: boolean
  /** 收藏：进入「收藏」智能清单 */
  starred: boolean
  completedAt: string | null
  sortOrder: number
  createdAt: string
  updatedAt: string
  subtasks: Subtask[]
  attachments: Attachment[]
  tags: Tag[]
  listName: string
  listColor: string
  folderId: number | null
  subtaskDone: number
  subtaskOpen: number
}

/** 任务附件。内容存在服务端数据目录，这里只描述它。 */
export interface Attachment {
  id: number
  taskId: number
  name: string
  file: string
  size: number
  mime: string
  createdAt: string
}

/** 出站 Webhook：任务变更时向外部地址推送一条 JSON。 */
export interface Webhook {
  id: number
  name: string
  url: string
  /** 列表接口一律脱敏，只有 hasSecret 表示是否配过签名密钥 */
  secret: string
  hasSecret: boolean
  events: string[]
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface WebhookDelivery {
  id: number
  webhookId: number
  event: string
  code: number
  ok: boolean
  error: string
  createdAt: string
}

/** 模板任务：反复要做的事存成底稿，一键铺开成真正的任务。 */
export interface TaskTemplate {
  id: number
  name: string
  title: string
  notes: string
  listId: number | null
  priority: Priority
  /** 相对生成日的天数偏移，null 表示不带日期 */
  dueOffset: number | null
  dueTime: string | null
  reminders: number[]
  repeatRule: string | null
  important: boolean
  urgent: boolean
  tagIds: number[]
  subtasks: string[]
  sortOrder: number
  createdAt: string
  updatedAt: string
}

/** 模板写入载荷，字段缺席表示不改动。 */
export interface TemplatePatch {
  name?: string
  title?: string
  notes?: string
  listId?: number | null
  priority?: Priority
  dueOffset?: number | null
  dueTime?: string | null
  reminders?: number[]
  repeatRule?: string | null
  important?: boolean
  urgent?: boolean
  tagIds?: number[]
  subtasks?: string[]
  sortOrder?: number
}

/** 自动备份的状态与设置。 */
export interface BackupStatus {
  enabled: boolean
  hour: string
  keep: string
  lastAt: string
  lastFile: string
  lastError: string
  dir: string
  files: string[]
}

/** 可订阅的 Webhook 事件。 */
export const WEBHOOK_EVENTS = [
  'task.created',
  'task.updated',
  'task.completed',
  'task.reopened',
  'task.deleted',
] as const

export const WEBHOOK_EVENT_LABEL: Record<string, string> = {
  'task.created': '新建任务',
  'task.updated': '任务被修改',
  'task.completed': '任务完成',
  'task.reopened': '任务恢复未完成',
  'task.deleted': '任务被删除',
}

export interface List {
  id: number
  folderId: number | null
  name: string
  color: string
  icon: string
  sortOrder: number
  archived: boolean
  starred: boolean
  createdAt: string
  taskCount: number
}

export interface Folder {
  id: number
  parentId: number | null
  name: string
  color: string
  icon: string
  sortOrder: number
  collapsed: boolean
  archived: boolean
  createdAt: string
  lists: List[]
  /** 子分组：分组可以再套分组 */
  children: Folder[]
}

export type SmartKey =
  | 'inbox'
  | 'today'
  | 'tomorrow'
  | 'week'
  | 'next7'
  | 'overdue'
  | 'nodate'
  | 'high'
  | 'starred'
  | 'updated'
  | 'recentdone'
  | 'all'
  | 'done'

/** 保存下来的筛选条件。query 是 TaskFilter 的 JSON 原文，由前端自行解释。 */
export interface SavedFilter {
  id: number
  name: string
  query: string
  sortOrder: number
  createdAt: string
}

/** 撤销槽位状态：最近一次删除是否还可以挽回。 */
export interface UndoState {
  available: boolean
  label: string
  count: number
  at: string
}

/** 一条操作历史。 */
export interface Activity {
  id: number
  kind: string
  taskId: number | null
  title: string
  detail: string
  createdAt: string
}

export const ACTIVITY_LABEL: Record<string, string> = {
  created: '新建',
  completed: '完成',
  reopened: '恢复',
  deleted: '删除',
  duplicated: '复制',
  moved: '移动',
  purged: '清空',
  undone: '撤销',
  archived: '归档',
  unarchived: '取消归档',
}

export interface Bootstrap {
  app: string
  motto: string
  today: string
  folders: Folder[]
  lists: List[]
  tags: Tag[]
  settings: Record<string, string>
  inboxListId: number
  counts: Record<string, number>
  savedFilters: SavedFilter[]
  undo: UndoState
}

export interface TrendPoint {
  date: string
  created: number
  done: number
  focus: number
}

export interface CountByKey {
  key: string
  label: string
  count: number
}

export interface Stats {
  totalOpen: number
  totalDone: number
  totalAll: number
  doneToday: number
  dueToday: number
  dueTodayDone: number
  overdue: number
  completion: number
  streakDays: number
  focusMinutes: number
  trend: TrendPoint[]
  byList: CountByKey[]
  byPriority: CountByKey[]
  byQuadrant: CountByKey[]
}

export interface Review {
  id: number
  date: string
  mood: string
  wins: string
  blockers: string
  tomorrow: string
  createdAt: string
  updatedAt: string
}

export interface FocusSession {
  id: number
  taskId: number | null
  taskTitle: string
  minutes: number
  startedAt: string
  endedAt: string
}

export interface ReminderHit {
  task: Task
  fireAt: string
  offset: number
  overdue: boolean
  dueLabel: string
}

export interface ToggleResult {
  task: Task
  nextTask: Task | null
  completed: boolean
}

export interface RepeatPreset {
  value: string
  label: string
  group: string
}

export interface RepeatMeta {
  presets: RepeatPreset[]
  ebbinghausOffsets: number[]
}

/** 列表视图类型 */
export type ViewKind = 'list' | 'board' | 'table' | 'calendar' | 'quadrant' | 'stats' | 'habits'

/** 当前侧边栏选中的目标 */
export type Selection =
  | { kind: 'smart'; key: SmartKey }
  | { kind: 'folder'; id: number }
  | { kind: 'list'; id: number }
  | { kind: 'tag'; id: number }
  | { kind: 'search'; q: string }

/** 习惯的重复节奏 */
export type HabitCadence = 'daily' | 'weekly'

/** 习惯：需要长期重复的修身之事，与一次性任务分开建模。 */
export interface Habit {
  id: number
  name: string
  icon: string
  color: string
  cadence: HabitCadence
  /** 逗号分隔，0=周日；仅 weekly 生效 */
  weekdays: string
  /** 单次达标所需打卡次数 */
  target: number
  /** 起始日 YYYY-MM-DD；之前的日期不计入欠账 */
  startDate: string
  note: string
  archived: boolean
  sortOrder: number
  createdAt: string
  updatedAt: string
}

export interface HabitLog {
  id: number
  habitId: number
  day: string
  count: number
  note: string
  createdAt: string
}

/** 单个习惯在区间内的统计 */
export interface HabitStat {
  habitId: number
  /** 当前连续（按节奏计，单位为「次」） */
  streak: number
  best: number
  done: number
  due: number
  rate: number
  today: boolean
  todayAt: number
}

export interface HabitBoard {
  from: string
  to: string
  today: string
  habits: Habit[]
  logs: HabitLog[]
  stats: HabitStat[]
}

/** 习惯写入载荷，字段缺席表示不改动。 */
export interface HabitPatch {
  name?: string
  icon?: string
  color?: string
  cadence?: HabitCadence
  weekdays?: string
  target?: number
  startDate?: string
  note?: string
  archived?: boolean
  sortOrder?: number
}

/** 任务写入载荷，字段缺席表示不改动。 */
export interface TaskPatch {
  title?: string
  notes?: string
  listId?: number
  status?: 'todo' | 'done'
  priority?: Priority
  startDate?: string | null
  dueDate?: string | null
  dueTime?: string | null
  endTime?: string | null
  url?: string
  reminders?: number[]
  repeatRule?: string | null
  repeatFrom?: 'due' | 'done'
  important?: boolean
  urgent?: boolean
  pinned?: boolean
  starred?: boolean
  tagIds?: number[]
  subtasks?: { title: string; done: boolean; sortOrder: number }[]
  sortOrder?: number
}

/** 批量操作的 action 取值。 */
export type BatchAction =
  | 'complete'
  | 'reopen'
  | 'delete'
  | 'move'
  | 'pin'
  | 'unpin'
  | 'star'
  | 'unstar'

/** 明暗模式。auto 表示跟随系统外观，由 AppStore 解析成实际生效的 light / dark。 */
export type ThemeMode = 'light' | 'dark' | 'auto'

/** 界面字号档位：作用于根元素 font-size，正文、间距随之等比缩放。 */
export const FONT_SCALES = [
  { value: '0.9', label: '紧凑', percent: '90%' },
  { value: '1', label: '标准', percent: '100%' },
  { value: '1.1', label: '舒适', percent: '110%' },
  { value: '1.2', label: '大号', percent: '120%' },
] as const

/** 把设置里的字号取值收敛到安全区间，非法值一律按标准（1）处理。 */
export function fontScaleOf(value?: string): number {
  const n = Number(value)
  return Number.isFinite(n) && n >= 0.75 && n <= 1.5 ? n : 1
}

export interface Settings {
  theme?: ThemeMode
  accent?: string
  fontScale?: string
  weekStart?: '0' | '1'
  showCompleted?: '0' | '1'
  soundOn?: '0' | '1'
  dailyFocus?: string
  morningPlanDone?: string
  reviewDone?: string
  priorityEnabled?: '0' | '1'
  [key: string]: string | undefined
}

export interface DailyFocus {
  date: string
  ids: number[]
}
