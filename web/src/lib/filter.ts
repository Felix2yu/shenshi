import type { Task } from '../types'

/** 工具栏筛选条件（与视图无关，作用于列表 / 看板 / 四象限 / 日历）。 */
export interface TaskFilter {
  priority: number | null
  tagId: number | null
}

export const EMPTY_FILTER: TaskFilter = { priority: null, tagId: null }

export function isFilterActive(f: TaskFilter): boolean {
  return f.priority !== null || f.tagId !== null
}

export function matchFilter(t: Task, f: TaskFilter): boolean {
  if (f.priority !== null && t.priority !== f.priority) return false
  if (f.tagId !== null && !t.tags.some((g) => g.id === f.tagId)) return false
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
  if (f.tagId !== null) parts.push(`#${tags.find((t) => t.id === f.tagId)?.name ?? ''}`)
  return parts.join(' · ')
}
