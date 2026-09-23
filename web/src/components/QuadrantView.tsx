import { useMemo, useState, type DragEvent } from 'react'

import { QUOTES } from '../lib/quotes'
import { matchFilter, type TaskFilter } from '../lib/filter'
import { useStore } from '../store/AppStore'
import type { Task } from '../types'
import { IconSparkle } from './icons'
import { DRAG_MIME, TaskRow } from './TaskViews'
import { EmptyState, cx } from './ui'

/**
 * 四象限（艾森豪威尔矩阵）。
 * 「重要」由优先级推导，「紧急」由到期时间推导，两者都可以在详情面板里手工改写。
 */
const QUADRANTS = [
  {
    key: 1,
    title: '重要且紧急',
    action: '立即做',
    hint: '《礼记·大学》：知止而后有定。先清此处，再谈其余。',
    accent: 'var(--p-high)',
    important: true,
    urgent: true,
  },
  {
    key: 2,
    title: '重要不紧急',
    action: '计划做',
    hint: '此处最见功夫 —— 慎始即在此时安排。',
    accent: 'var(--p-mid)',
    important: true,
    urgent: false,
  },
  {
    key: 3,
    title: '紧急不重要',
    action: '尽快处理',
    hint: '可委托、可合并，勿让其挤占第一象限。',
    accent: 'var(--p-low)',
    important: false,
    urgent: true,
  },
  {
    key: 4,
    title: '不紧急不重要',
    action: '择时或舍弃',
    hint: '张而不弛，文武弗能也。留白亦是一种节奏。',
    accent: 'var(--ink-3)',
    important: false,
    urgent: false,
  },
] as const

export function QuadrantView({ onOpen, filter }: { onOpen: (t: Task) => void; filter: TaskFilter }) {
  const { tasks, updateTask } = useStore()
  const [dropKey, setDropKey] = useState<number | null>(null)

  const grouped = useMemo(() => {
    const map = new Map<number, Task[]>()
    for (const q of QUADRANTS) map.set(q.key, [])
    for (const t of tasks) {
      if (t.status === 'done' || !matchFilter(t, filter)) continue
      const key = t.important ? (t.urgent ? 1 : 2) : t.urgent ? 3 : 4
      map.get(key)!.push(t)
    }
    for (const arr of map.values()) {
      arr.sort((a, b) => {
        if (a.dueDate && b.dueDate && a.dueDate !== b.dueDate) return a.dueDate < b.dueDate ? -1 : 1
        if (a.dueDate && !b.dueDate) return -1
        if (!a.dueDate && b.dueDate) return 1
        return b.priority - a.priority
      })
    }
    return map
  }, [tasks, filter])

  const onDrop = async (e: DragEvent<HTMLDivElement>, q: (typeof QUADRANTS)[number]) => {
    e.preventDefault()
    setDropKey(null)
    const id = Number(e.dataTransfer.getData(DRAG_MIME))
    if (!id) return
    await updateTask(id, { important: q.important, urgent: q.urgent })
  }

  const total = tasks.filter((t) => t.status === 'todo' && matchFilter(t, filter)).length

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-line px-5 py-2.5">
        <IconSparkle size={15} className="text-seal" />
        <h2 className="brand-serif text-[0.9375rem] font-semibold text-ink">四象限</h2>
        <span className="text-[0.71875rem] text-ink-3">
          共 {total} 项未完成 · 拖动任务卡片即可在象限之间移动
        </span>
      </div>

      <div className="flex-1 overflow-auto p-4">
        <div className="grid min-h-full grid-cols-1 gap-3 lg:grid-cols-2">
          {QUADRANTS.map((q) => {
            const list = grouped.get(q.key) ?? []
            return (
              <div
                key={q.key}
                onDragOver={(e) => {
                  e.preventDefault()
                  setDropKey(q.key)
                }}
                onDragLeave={() => setDropKey(null)}
                onDrop={(e) => void onDrop(e, q)}
                className={cx(
                  'flex min-h-[220px] flex-col rounded-2xl border border-line bg-surface/60 p-3 transition-colors',
                  dropKey === q.key && 'drop-target',
                )}
              >
                <div className="mb-2 flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-sm" style={{ background: q.accent }} />
                  <h3 className="text-[0.84375rem] font-semibold text-ink">{q.title}</h3>
                  <span className="rounded-full bg-surface-2 px-1.5 text-[0.6875rem] text-ink-3 tabular-nums">
                    {list.length}
                  </span>
                  <span className="ml-auto text-[0.71875rem] text-ink-3">{q.action}</span>
                </div>
                <p className="mb-2 px-0.5 text-[0.71875rem] leading-relaxed text-ink-3">{q.hint}</p>

                <div className="flex-1 space-y-0.5">
                  {list.length === 0 ? (
                    <div className="grid h-full place-items-center rounded-xl border border-dashed border-line py-6">
                      <span className="text-[0.71875rem] text-ink-3">把任务拖到此处</span>
                    </div>
                  ) : (
                    list.map((t) => <TaskRow key={t.id} task={t} onOpen={onOpen} dense showList />)
                  )}
                </div>
              </div>
            )
          })}
        </div>

        {tasks.filter((t) => t.status === 'todo').length === 0 ? (
          <div className="pb-10">
            <EmptyState
              text={QUOTES.quadrant.text}
              source={QUOTES.quadrant.source}
              hint="还没有未完成的任务，先记下一件事吧。"
            />
          </div>
        ) : null}
      </div>
    </div>
  )
}
