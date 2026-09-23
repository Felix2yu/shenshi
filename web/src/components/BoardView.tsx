import { useMemo, useState, type DragEvent } from 'react'

import { QUOTES } from '../lib/quotes'
import { matchFilter, type TaskFilter } from '../lib/filter'
import { useStore } from '../store/AppStore'
import type { Task } from '../types'
import { IconColumns } from './icons'
import { DRAG_MIME, TaskRow } from './TaskViews'
import { cx } from './ui'

type GroupBy = 'priority' | 'list' | 'tag' | 'due'

const GROUPS: { key: GroupBy; label: string }[] = [
  { key: 'priority', label: '按优先级' },
  { key: 'list', label: '按清单' },
  { key: 'tag', label: '按标签' },
  { key: 'due', label: '按时间' },
]

interface Column {
  key: string
  label: string
  color?: string
  tasks: Task[]
}

/** 看板视图：与列表共用同一批任务，只是换一种「横向」的读法。 */
export function BoardView({ onOpen, filter }: { onOpen: (t: Task) => void; filter: TaskFilter }) {
  const { tasks, tags, lists, updateTask } = useStore()
  const [groupBy, setGroupBy] = useState<GroupBy>('priority')
  const [dropCol, setDropCol] = useState<string | null>(null)

  const columns = useMemo<Column[]>(() => {
    const open = tasks.filter((t) => t.status === 'todo' && matchFilter(t, filter))

    if (groupBy === 'priority') {
      return [3, 2, 1, 0].map((p) => ({
        key: String(p),
        label: ['无优先级', '低优先级', '中优先级', '高优先级'][p],
        color: p === 3 ? 'var(--p-high)' : p === 2 ? 'var(--p-mid)' : p === 1 ? 'var(--p-low)' : 'var(--ink-3)',
        tasks: open.filter((t) => t.priority === p),
      }))
    }

    if (groupBy === 'list') {
      return lists.map((l) => ({
        key: `list-${l.id}`,
        label: l.name,
        color: l.color,
        tasks: open.filter((t) => t.listId === l.id),
      }))
    }

    if (groupBy === 'tag') {
      const cols: Column[] = tags.map((g) => ({
        key: `tag-${g.id}`,
        label: `#${g.name}`,
        color: g.color,
        tasks: open.filter((t) => t.tags.some((x) => x.id === g.id)),
      }))
      cols.push({ key: 'tag-none', label: '无标签', tasks: open.filter((t) => t.tags.length === 0) })
      return cols
    }

    // 按时间
    const today = new Date()
    const pad = (v: number) => String(v).padStart(2, '0')
    const str = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    const t0 = str(today)
    const t1 = str(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1))
    const t7 = str(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 7))
    return [
      { key: 'overdue', label: '逾期', color: 'var(--p-high)', tasks: open.filter((t) => t.dueDate && t.dueDate < t0) },
      { key: 'today', label: '今天', color: 'var(--seal)', tasks: open.filter((t) => t.dueDate === t0) },
      { key: 'tomorrow', label: '明天', color: 'var(--p-mid)', tasks: open.filter((t) => t.dueDate === t1) },
      {
        key: 'week',
        label: '本周内',
        tasks: open.filter((t) => t.dueDate && t.dueDate > t1 && t.dueDate <= t7),
      },
      { key: 'later', label: '更远', tasks: open.filter((t) => t.dueDate && t.dueDate > t7) },
      { key: 'none', label: '无日期', tasks: open.filter((t) => !t.dueDate) },
    ]
  }, [tasks, groupBy, lists, tags, filter])

  const onDrop = async (e: DragEvent<HTMLDivElement>, col: Column) => {
    e.preventDefault()
    setDropCol(null)
    const id = Number(e.dataTransfer.getData(DRAG_MIME))
    if (!id) return
    if (groupBy === 'priority') {
      await updateTask(id, { priority: Number(col.key) as 0 | 1 | 2 | 3 })
      return
    }
    if (groupBy === 'list' && col.key.startsWith('list-')) {
      await updateTask(id, { listId: Number(col.key.slice(5)) })
      return
    }
    if (groupBy === 'tag' && col.key.startsWith('tag-')) {
      const tagId = Number(col.key.slice(4))
      if (Number.isFinite(tagId)) {
        const task = tasks.find((t) => t.id === id)
        if (task && !task.tags.some((x) => x.id === tagId)) {
          await updateTask(id, { tagIds: [...task.tags.map((t) => t.id), tagId] })
        }
      }
      return
    }
    if (groupBy === 'due') {
      const today = new Date()
      const pad = (v: number) => String(v).padStart(2, '0')
      const str = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
      const map: Record<string, string | null> = {
        overdue: str(new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)),
        today: str(today),
        tomorrow: str(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1)),
        week: str(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 3)),
        later: str(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 14)),
        none: null,
      }
      await updateTask(id, { dueDate: map[col.key] ?? null })
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-line px-5 py-2.5">
        <IconColumns size={15} className="text-seal" />
        <h2 className="brand-serif text-[0.9375rem] font-semibold text-ink">看板</h2>
        <span className="hidden text-[0.71875rem] text-ink-3 sm:block">拖动卡片可跨列调整</span>
        <div className="ml-auto flex rounded-lg border border-line p-0.5">
          {GROUPS.map((g) => (
            <button
              key={g.key}
              type="button"
              onClick={() => setGroupBy(g.key)}
              className={cx(
                'rounded-md px-2.5 py-1 text-[0.78125rem] transition-colors',
                groupBy === g.key ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
              )}
            >
              {g.label}
            </button>
          ))}
        </div>
      </div>

      <div className="flex-1 overflow-x-auto overflow-y-hidden p-4">
        <div className="flex h-full min-w-min gap-3">
          {columns.map((col) => (
            <div
              key={col.key}
              onDragOver={(e) => {
                e.preventDefault()
                setDropCol(col.key)
              }}
              onDragLeave={() => setDropCol(null)}
              onDrop={(e) => void onDrop(e, col)}
              className={cx(
                'flex h-full w-[276px] shrink-0 flex-col rounded-2xl border border-line bg-surface/60 p-2.5',
                dropCol === col.key && 'drop-target',
              )}
            >
              <div className="mb-2 flex items-center gap-2 px-1">
                <span
                  className="h-2 w-2 rounded-full"
                  style={{ background: col.color ?? 'var(--ink-3)' }}
                />
                <span className="text-[0.8125rem] font-medium text-ink">{col.label}</span>
                <span className="rounded-full bg-surface-2 px-1.5 text-[0.6875rem] text-ink-3 tabular-nums">
                  {col.tasks.length}
                </span>
              </div>
              <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto">
                {col.tasks.length === 0 ? (
                  <div className="grid h-24 place-items-center rounded-xl border border-dashed border-line">
                    <span className="text-[0.71875rem] text-ink-3">拖到此处</span>
                  </div>
                ) : (
                  col.tasks.map((t) => <TaskRow key={t.id} task={t} onOpen={onOpen} dense showList />)
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      {tasks.filter((t) => t.status === 'todo').length === 0 ? (
        <div className="pb-12">
          <div className="text-center">
            <p className="brand-serif text-[0.875rem] text-ink-2">{QUOTES.board.text}</p>
            <p className="mt-1 text-[0.71875rem] text-ink-3">{QUOTES.board.source}</p>
            <p className="mt-3 text-[0.78125rem] text-ink-3">当前范围内没有未完成的任务。</p>
          </div>
        </div>
      ) : null}
    </div>
  )
}
