import { useMemo } from 'react'

import { dueLabel, relativeTime, todayStr } from '../lib/date'
import { useStore } from '../store/AppStore'
import { PRIORITY_LABEL, type Task } from '../types'
import { IconCheckCircle, IconClock, IconLink, IconPin, IconStar } from './icons'
import { EmptyState, RoundCheck, cx } from './ui'
import type { TaskSort } from '../api/client'

/** 列头可以点的列：点一次换排序字段。 */
const SORTABLE: Record<string, TaskSort> = {
  title: 'title',
  priority: 'priority',
  dueDate: 'due',
  updatedAt: 'updated',
}

const PRIORITY_TONE: Record<number, string> = {
  3: 'bg-[var(--p-high)]/12 text-[var(--p-high)]',
  2: 'bg-[var(--p-mid)]/14 text-[var(--p-mid)]',
  1: 'bg-[var(--p-low)]/14 text-[var(--p-low)]',
  0: 'text-ink-3',
}

/**
 * 表格视图：所有字段摊平在一张表上，便于横向比对与批量核对。
 * 它与列表视图共用同一份筛选结果，只是换了个摆放方式——不做第二套数据通路。
 */
export function TableView({ tasks, onOpen }: { tasks: Task[]; onOpen: (t: Task) => void }) {
  const { toggleTask, sortBy, setSortBy, lists } = useStore()

  const listName = useMemo(() => {
    const m = new Map<number, string>()
    for (const l of lists) m.set(l.id, l.name)
    return (t: Task) => t.listName || m.get(t.listId) || '—'
  }, [lists])

  if (!tasks.length) {
    return (
      <div className="p-6">
        <EmptyState text="这张表还没有内容" hint="换个清单，或把筛选放宽一些。" />
      </div>
    )
  }

  const today = todayStr()

  return (
    <div className="min-h-full overflow-x-auto">
      <table data-table className="w-full min-w-[860px] border-collapse text-[0.78125rem]">
        <thead className="sticky top-0 z-10 bg-paper/95 backdrop-blur">
          <tr className="border-b border-line text-left text-[0.6875rem] text-ink-3">
            <th className="w-9 px-2 py-2 font-normal" />
            <Th col="title" label="任务" sortBy={sortBy} onSort={setSortBy} className="min-w-[240px]" />
            <th className="w-[132px] px-2 py-2 font-normal">清单</th>
            <Th col="priority" label="优先级" sortBy={sortBy} onSort={setSortBy} className="w-[74px]" />
            <th className="w-[70px] px-2 py-2 font-normal">状态</th>
            <th className="w-[104px] px-2 py-2 font-normal">开始</th>
            <Th col="dueDate" label="到期" sortBy={sortBy} onSort={setSortBy} className="w-[116px]" />
            <th className="w-[170px] px-2 py-2 font-normal">标签</th>
            <Th col="updatedAt" label="更新于" sortBy={sortBy} onSort={setSortBy} className="w-[104px]" />
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => {
            const overdue = t.status === 'todo' && !!t.dueDate && t.dueDate < today
            const done = t.status === 'done'
            return (
              <tr
                key={t.id}
                data-table-row={t.id}
                onClick={() => onOpen(t)}
                className={cx(
                  'cursor-pointer border-b border-line/60 transition-colors hover:bg-surface-2/60',
                  done && 'text-ink-3',
                )}
              >
                <td className="px-2 py-1.5 align-middle">
                  <RoundCheck
                    checked={done}
                    onChange={() => void toggleTask(t.id)}
                    color={t.listColor || undefined}
                    size={16}
                  />
                </td>
                <td className="px-2 py-1.5 align-middle">
                  <div className="flex items-center gap-1.5">
                    {t.pinned ? <IconPin size={12} className="shrink-0 text-seal" /> : null}
                    {t.starred ? <IconStar size={12} className="shrink-0 text-[var(--p-mid)]" /> : null}
                    <span className={cx('truncate', done && 'line-through')}>{t.title}</span>
                    {t.url ? <IconLink size={11} className="shrink-0 text-ink-3" /> : null}
                    {t.repeatRule ? <IconClock size={11} className="shrink-0 text-ink-3" /> : null}
                    {t.subtasks.length ? (
                      <span className="shrink-0 text-[0.65625rem] text-ink-3">
                        子 {t.subtasks.filter((s) => s.done).length}/{t.subtasks.length}
                      </span>
                    ) : null}
                  </div>
                </td>
                <td className="px-2 py-1.5 align-middle">
                  <span className="flex items-center gap-1.5 text-ink-2">
                    <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: t.listColor || 'var(--line)' }} />
                    <span className="truncate">{listName(t)}</span>
                  </span>
                </td>
                <td className="px-2 py-1.5 align-middle">
                  <span className={cx('rounded px-1.5 py-0.5 text-[0.6875rem]', PRIORITY_TONE[t.priority])}>
                    {PRIORITY_LABEL[t.priority]}
                  </span>
                </td>
                <td className="px-2 py-1.5 align-middle">
                  {done ? (
                    <span className="inline-flex items-center gap-1 text-jade">
                      <IconCheckCircle size={12} />
                      已完成
                    </span>
                  ) : (
                    <span className="text-ink-2">未完成</span>
                  )}
                </td>
                <td className="px-2 py-1.5 align-middle tabular-nums text-ink-2">{t.startDate ?? '—'}</td>
                <td className="px-2 py-1.5 align-middle">
                  <span className={cx('tabular-nums', overdue ? 'text-p-high' : 'text-ink-2')}>
                    {t.dueDate ? `${t.dueDate}${t.dueTime ? ` ${t.dueTime}` : ''}` : '—'}
                  </span>
                  {t.dueDate ? <span className="ml-1 text-[0.65625rem] text-ink-3">{dueLabel(t.dueDate)}</span> : null}
                </td>
                <td className="px-2 py-1.5 align-middle">
                  <span className="flex flex-wrap gap-1">
                    {t.tags.length === 0 ? (
                      <span className="text-ink-3">—</span>
                    ) : (
                      t.tags.map((g) => (
                        <span
                          key={g.id}
                          className="rounded px-1.5 py-0.5 text-[0.65625rem]"
                          style={{ background: `${g.color}1f`, color: g.color }}
                        >
                          {g.name}
                        </span>
                      ))
                    )}
                  </span>
                </td>
                <td className="px-2 py-1.5 align-middle text-[0.71875rem] text-ink-3">{relativeTime(t.updatedAt)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <p className="px-3 py-2 text-[0.6875rem] text-ink-3">
        共 {tasks.length} 行 · 点列头可换排序，点任意一行打开详情
      </p>
    </div>
  )
}

function Th({
  col,
  label,
  sortBy,
  onSort,
  className,
}: {
  col: string
  label: string
  sortBy: TaskSort
  onSort: (v: TaskSort) => void
  className?: string
}) {
  const target = SORTABLE[col]
  const active = sortBy === target
  return (
    <th className={cx('px-2 py-2 font-normal', className)}>
      <button
        type="button"
        data-table-sort={col}
        onClick={() => onSort(target)}
        className={cx(
          'inline-flex items-center gap-1 rounded px-1 py-0.5 transition-colors',
          active ? 'font-medium text-seal' : 'text-ink-3 hover:text-ink',
        )}
      >
        {label}
        {active ? <IconCheckCircle size={11} /> : null}
      </button>
    </th>
  )
}
