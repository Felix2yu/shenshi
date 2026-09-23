import type { Task } from '../types'

/** 完成状态筛选。默认只看未完成——「全部」是用户主动要的视角，不该是默认。 */
export type StatusFilter = 'all' | 'todo' | 'done'

/** 多标签之间的组合方式：任一命中，还是全部命中。 */
export type TagMode = 'any' | 'all'

/**
 * 工具栏筛选条件（与视图无关，作用于列表 / 看板 / 表格 / 四象限 / 日历）。
 * 这一组条件是纯前端的——服务端的 search 只做关键词与结构化查询，
 * 交互态的筛选留在本地，改一次不必往返一次网络。
 */
export interface TaskFilter {
  priority: number | null
  tagIds: number[]
  tagMode: TagMode
  status: StatusFilter
  /** 到期日区间（含端点），YYYY-MM-DD */
  from: string | null
  to: string | null
  pinned: boolean
  starred: boolean
}

export const EMPTY_FILTER: TaskFilter = {
  priority: null,
  tagIds: [],
  tagMode: 'any',
  status: 'all',
  from: null,
  to: null,
  pinned: false,
  starred: false,
}

/**
 * 兼容早期版本存下来的 `{priority, tagId}` 形状。
 * 保存的筛选条件是持久化在服务端的，升级后不能让旧的记录变成一团乱码。
 */
function normalize(raw: Partial<TaskFilter> & { tagId?: number | null }): TaskFilter {
  const tagIds = Array.isArray(raw.tagIds)
    ? raw.tagIds.filter((n): n is number => typeof n === 'number')
    : typeof raw.tagId === 'number'
      ? [raw.tagId]
      : []
  return {
    priority: typeof raw.priority === 'number' ? raw.priority : null,
    tagIds,
    tagMode: raw.tagMode === 'all' ? 'all' : 'any',
    status: raw.status === 'todo' || raw.status === 'done' ? raw.status : 'all',
    from: typeof raw.from === 'string' && raw.from ? raw.from : null,
    to: typeof raw.to === 'string' && raw.to ? raw.to : null,
    pinned: raw.pinned === true,
    starred: raw.starred === true,
  }
}

export function isFilterActive(f: TaskFilter): boolean {
  return (
    f.priority !== null ||
    f.tagIds.length > 0 ||
    f.status !== 'all' ||
    f.from !== null ||
    f.to !== null ||
    f.pinned ||
    f.starred
  )
}

export function matchFilter(t: Task, f: TaskFilter): boolean {
  if (f.priority !== null && t.priority !== f.priority) return false
  if (f.status !== 'all' && t.status !== f.status) return false
  if (f.pinned && !t.pinned) return false
  if (f.starred && !t.starred) return false
  // 设了区间就只在有到期日的任务里找——没有日期的任务谈不上「落在哪一段」。
  if (f.from !== null || f.to !== null) {
    if (!t.dueDate) return false
    if (f.from !== null && t.dueDate < f.from) return false
    if (f.to !== null && t.dueDate > f.to) return false
  }
  if (f.tagIds.length > 0) {
    const owned = new Set(t.tags.map((g) => g.id))
    const hit = f.tagMode === 'all' ? f.tagIds.every((id) => owned.has(id)) : f.tagIds.some((id) => owned.has(id))
    if (!hit) return false
  }
  return true
}

export function applyFilter(tasks: Task[], f: TaskFilter): Task[] {
  if (!isFilterActive(f)) return tasks
  return tasks.filter((t) => matchFilter(t, f))
}

/** 人类可读的筛选描述，用于提示条。 */
export function describeFilter(f: TaskFilter, tags: { id: number; name: string }[]): string {
  const parts: string[] = []
  if (f.priority !== null) parts.push(`优先级 ${['无', '低', '中', '高'][f.priority] ?? f.priority}`)
  if (f.status !== 'all') parts.push(f.status === 'done' ? '仅已完成' : '仅未完成')
  if (f.from !== null || f.to !== null) {
    if (f.from !== null && f.to !== null) parts.push(`${f.from} → ${f.to}`)
    else if (f.from !== null) parts.push(`${f.from} 起`)
    else parts.push(`至 ${f.to}`)
  }
  if (f.tagIds.length > 0) {
    const names = f.tagIds.map((id) => `#${tags.find((t) => t.id === id)?.name ?? id}`)
    parts.push(f.tagIds.length > 1 ? `${names.join(f.tagMode === 'all' ? ' 且 ' : ' 或 ')}` : names[0])
  }
  if (f.pinned) parts.push('已置顶')
  if (f.starred) parts.push('已收藏')
  return parts.join(' · ')
}

/** 落库前的整理：把等价的空值归一，避免「看起来一样但 JSON 不同」的重复记录。 */
export function filterToQuery(f: TaskFilter): string {
  return JSON.stringify(normalize(f))
}

export function filterFromQuery(query: string): TaskFilter {
  try {
    const raw = JSON.parse(query) as Partial<TaskFilter> & { tagId?: number | null }
    return normalize(raw ?? {})
  } catch {
    return EMPTY_FILTER
  }
}
