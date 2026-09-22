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
  dueDate: string | null
  dueTime: string | null
  endTime: string | null
  reminders: number[]
  repeatRule: string | null
  important: boolean
  urgent: boolean
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
  createdAt: string
  taskCount: number
}

export interface Folder {
  id: number
  name: string
  color: string
  icon: string
  sortOrder: number
  collapsed: boolean
  createdAt: string
  lists: List[]
}

export type SmartKey =
  | 'inbox'
  | 'today'
  | 'next7'
  | 'overdue'
  | 'nodate'
  | 'all'
  | 'done'

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
export type ViewKind = 'list' | 'board' | 'calendar' | 'quadrant' | 'stats' | 'habits'

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
  dueDate?: string | null
  dueTime?: string | null
  endTime?: string | null
  reminders?: number[]
  repeatRule?: string | null
  important?: boolean
  urgent?: boolean
  tagIds?: number[]
  subtasks?: { title: string; done: boolean; sortOrder: number }[]
  sortOrder?: number
}

export interface Settings {
  theme?: 'light' | 'dark'
  accent?: string
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
