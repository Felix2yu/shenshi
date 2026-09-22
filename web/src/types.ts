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
  tags: Tag[]
  listName: string
  listColor: string
  folderId: number | null
  subtaskDone: number
  subtaskOpen: number
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
