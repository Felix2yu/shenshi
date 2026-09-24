import { useMemo, useState, type DragEvent } from 'react'

import { QUOTES } from '../lib/quotes'
import { matchFilter, type TaskFilter } from '../lib/filter'
import { useStore } from '../store/AppStore'
import type { Task, TaskPatch } from '../types'
import { IconColumns, IconPlus } from './icons'
import { DRAG_MIME, QuickAdd, TaskRow } from './TaskViews'
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

/** 列头新建任务的预填字段：与该列的归组语义保持一致。 */
function colDefaults(groupBy: GroupBy, col: Column): TaskPatch | undefined {
  if (groupBy === 'priority') return { priority: Number(col.key) as 0 | 1 | 2 | 3 }
  if (groupBy === 'list' && col.key.startsWith('list-')) return { listId: Number(col.key.slice(5)) }
  if (groupBy === 'tag' && col.key.startsWith('tag-')) {
    const id = Number(col.key.slice(4))
    return Number.isFinite(id) && id > 0 ? { tagIds: [id] } : undefined
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
    if (col.key in map) return { dueDate: map[col.key] }
  }
  return undefined
}

/** 看板视图：与列表共用同一批任务，只是换一种「横向」的读法。 */
export function BoardView({ onOpen, filter }: { onOpen: (t: Task) => void; filter: TaskFilter }) {
  const { tasks, tags, lists, updateTask, toast } = useStore()
  const [groupBy, setGroupBy] = useState<GroupBy>('priority')
  const [dropCol, setDropCol] = useState<string | null>(null)
  const [quickCol, setQuickCol] = useState<string | null>(null)
  // 拖拽起点所在列。按标签分组时同一个任务会出现在多列，
  // 只有知道"从哪一列拖出来的"才能在放下列时正确替换而不是叠加。
  const [dragFrom, setDragFrom] = useState<string | null>(null)

  const columns = useMemo<Column[]>(() => {
    const open = tasks.filter((t) => t.status !== 'done' && matchFilter(t, filter))

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
    const from = dragFrom
    setDragFrom(null)
    const id = Number(e.dataTransfer.getData(DRAG_MIME))
    if (!id) return
    if (groupBy === 'priority') {
      const p = Number(col.key) as 0 | 1 | 2 | 3
      await updateTask(id, { priority: p })
      toast(`优先级已改为「${col.label}」`)
      return
    }
    if (groupBy === 'list' && col.key.startsWith('list-')) {
      await updateTask(id, { listId: Number(col.key.slice(5)) })
      toast(`已移入「${col.label}」`)
      return
    }
    if (groupBy === 'tag') {
      const task = tasks.find((t) => t.id === id)
      if (!task) return
      const current = task.tags.map((x) => x.id)
      if (col.key === 'tag-none') {
        // 「无标签」列：此前因为没有数字后缀，Number('') 得 NaN 后被静默忽略，
        // 卡片弹回原位却毫无提示。现在明确表达"清空标签"。
        if (current.length === 0) return
        await updateTask(id, { tagIds: [] })
        toast('已移除全部标签')
        return
      }
      if (!col.key.startsWith('tag-')) return
      const tagId = Number(col.key.slice(4))
      if (!Number.isFinite(tagId)) return
      // 从别的标签列拖过来算「换列」：移除来源列的标签再加目标标签。
      // 只做追加的话，卡片会同时留在两列里，与"拖到哪列就归哪列"相悖。
      const fromTagId = from && from.startsWith('tag-') && from !== 'tag-none' ? Number(from.slice(4)) : NaN
      const base = Number.isFinite(fromTagId) ? current.filter((x) => x !== fromTagId) : current
      const next = Array.from(new Set(base.concat(tagId)))
      if (next.length === current.length) return
      await updateTask(id, { tagIds: next })
      toast(`已加入「${col.label}」`)
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
      toast(map[col.key] ? `到期日已改为 ${map[col.key]}` : '已清除到期日')
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-line px-3 py-2.5 md:px-5">
        <IconColumns size={15} className="shrink-0 text-seal" />
        <h2 className="brand-serif shrink-0 whitespace-nowrap text-[0.9375rem] font-semibold text-ink">看板</h2>
        <span className="hidden text-[0.71875rem] text-ink-3 sm:block">拖动卡片可跨列调整</span>
        {/* 分组切换：min-w-0 flex-1 让它在窄屏吃掉剩余宽度并内部横向滚动；
            原先 shrink-0 会按内容宽度撑破 375px 视口（实测 86→407）。桌面端 sm:flex-none 靠右。 */}
        <div className="ml-auto flex min-w-0 flex-1 justify-end overflow-x-auto rounded-lg border border-line p-0.5 no-scrollbar sm:flex-none">
          {GROUPS.map((g) => (
            <button
              key={g.key}
              type="button"
              onClick={() => setGroupBy(g.key)}
              className={cx(
                'shrink-0 whitespace-nowrap rounded-md px-2.5 py-1 text-[0.78125rem] transition-colors',
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
              onDragStart={() => setDragFrom(col.key)}
              onDragOver={(e) => {
                e.preventDefault()
                // dragover 以 ~60Hz 触发：只在目标列真的变化时才 setState，
                // 否则拖拽全程整块看板都在重渲染
                if (dropCol !== col.key) setDropCol(col.key)
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
                <button
                  type="button"
                  title="在此列新建任务"
                  onClick={() => setQuickCol(quickCol === col.key ? null : col.key)}
                  className={cx(
                    'ml-auto rounded-md p-0.5 text-ink-3 transition-colors hover:bg-surface-2 hover:text-seal',
                    quickCol === col.key && 'bg-seal/10 text-seal',
                  )}
                >
                  <IconPlus size={13} />
                </button>
              </div>
              {quickCol === col.key ? (
                <div className="mb-2">
                  <QuickAdd autoFocus placeholder="记一件事…" defaults={colDefaults(groupBy, col)} />
                </div>
              ) : null}
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

      {/* 空态与列内数据同口径：套上筛选条件，否则筛掉全部内容时会显示"看板为空"的误导文案 */}
      {tasks.filter((t) => t.status !== 'done' && matchFilter(t, filter)).length === 0 ? (
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
