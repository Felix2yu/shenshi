import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react'

import { dayDiff, dueLabel, isOverdue, relativeTime, todayStr, addDays } from '../lib/date'
import { QUOTES } from '../lib/quotes'
import { parseQuickAdd, describeRepeat, type Chip } from '../lib/nlp'
import { useIMEGuard } from '../lib/ime'
import { useStore } from '../store/AppStore'
import type { Priority, Selection, Task } from '../types'
import {
  IconBell,
  IconCheck,
  IconCircle,
  IconClock,
  IconCopy,
  IconFlag,
  IconGrip,
  IconList,
  IconMore,
  IconMove,
  IconNote,
  IconPencil,
  IconPin,
  IconPlus,
  IconRepeat,
  IconSearch,
  IconSparkle,
  IconStar,
  IconSubtask,
  IconTag,
  IconTrash,
  IconX,
  type IconProps,
} from './icons'
import { Button, Checkbox, EmptyState, IconButton, Popover, RoundCheck, cx } from './ui'

export const DRAG_MIME = 'application/x-shenshi-task'

/** 手动排序专用：与跨清单拖拽区分开，避免拖动行时误触发移动语义。 */
export const SORT_MIME = 'application/x-shenshi-sort'

type IconCmp = (p: IconProps) => React.ReactNode

/* ---------------- 小组件 ---------------- */

export function PriorityFlag({ priority, size = 13 }: { priority: Priority; size?: number }) {
  if (!priority) return null
  const color = priority === 3 ? 'var(--p-high)' : priority === 2 ? 'var(--p-mid)' : 'var(--p-low)'
  return (
    <span title={`优先级：${['无', '低', '中', '高'][priority]}`} className="inline-flex shrink-0">
      <IconFlag size={size} style={{ color }} strokeWidth={priority === 3 ? 2.2 : 1.7} />
    </span>
  )
}

export function DuePill({ task, className }: { task: Task; className?: string }) {
  if (!task.dueDate) return null
  const overdue = isOverdue(task.dueDate, task.status)
  const today = dayDiff(task.dueDate, todayStr()) === 0
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1 whitespace-nowrap text-[0.71875rem] tabular-nums',
        overdue ? 'font-medium text-p-high' : today ? 'text-seal' : 'text-ink-3',
        className,
      )}
    >
      <IconClock size={11.5} />
      {dueLabel(task.dueDate, task.dueTime)}
    </span>
  )
}

export function TaskMeta({ task }: { task: Task }) {
  const repeatLabel = describeRepeat(task.repeatRule)
  return (
    <div className="mt-0.5 flex flex-wrap items-center gap-x-2.5 gap-y-1">
      {task.notes ? (
        <span title="有备注" className="text-ink-3">
          <IconNote size={11.5} />
        </span>
      ) : null}
      {task.dueDate ? <DuePill task={task} /> : null}
      {task.endTime ? <span className="text-[0.71875rem] text-ink-3 tabular-nums">→ {task.endTime}</span> : null}
      {task.repeatRule ? (
        <span title={repeatLabel} className="inline-flex items-center gap-1 text-[0.71875rem] text-ink-3">
          <IconRepeat size={11.5} />
          <span className="hidden sm:inline">{repeatLabel}</span>
        </span>
      ) : null}
      {task.reminders.length > 0 && task.dueDate ? (
        <span title="已设提醒" className="inline-flex items-center gap-1 text-[0.71875rem] text-ink-3">
          <IconBell size={11.5} />
          {task.reminders.some((r) => r > 0) ? task.reminders.filter((r) => r > 0).map((r) => (r >= 60 ? `${r / 60}时` : `${r}分`)).join('/') : '准点'}
        </span>
      ) : null}
      {task.subtasks.length > 0 ? (
        <span
          title="子任务进度"
          className={cx(
            'inline-flex items-center gap-1 text-[0.71875rem] tabular-nums',
            task.subtaskOpen === 0 ? 'text-jade' : 'text-ink-3',
          )}
        >
          <IconSubtask size={11.5} />
          {task.subtaskDone}/{task.subtasks.length}
        </span>
      ) : null}
      {task.tags.map((t) => (
        <span key={t.id} className="inline-flex items-center gap-1 text-[0.71875rem]" style={{ color: t.color }}>
          <IconTag size={11} />#{t.name}
        </span>
      ))}
    </div>
  )
}

/* ---------------- 任务行 ---------------- */

export function TaskRow({
  task,
  onOpen,
  draggable = true,
  dense,
  showList,
  sortable,
  onSortStart,
  onSortEnd,
}: {
  task: Task
  onOpen: (t: Task) => void
  draggable?: boolean
  dense?: boolean
  showList?: boolean
  /** 手动排序模式：行首出现拖拽手柄。 */
  sortable?: boolean
  onSortStart?: (id: number) => void
  onSortEnd?: () => void
}) {
  const { toggleTask, updateTask, deleteTask, multiSelect, selectedIds, toggleSelected, selectedTaskId } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(task.title)
  const [menuOpen, setMenuOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const done = task.status === 'done'

  useEffect(() => {
    if (editing) inputRef.current?.select()
  }, [editing])

  useEffect(() => {
    if (!editing) setDraft(task.title)
  }, [task.title, editing])

  const commit = async () => {
    const value = draft.trim()
    setEditing(false)
    if (value && value !== task.title) await updateTask(task.id, { title: value })
  }

  const onDragStart = (e: DragEvent<HTMLDivElement>) => {
    e.dataTransfer.setData(DRAG_MIME, String(task.id))
    e.dataTransfer.setData('text/plain', task.title)
    e.dataTransfer.effectAllowed = 'move'
  }

  return (
    <div
      data-task-row={task.id}
      draggable={draggable && !editing}
      onDragStart={onDragStart}
      onClick={() => (multiSelect ? toggleSelected(task.id) : onOpen(task))}
      className={cx(
        'group/row relative flex cursor-pointer items-start gap-2.5 rounded-xl border px-3 transition-colors',
        dense ? 'py-1.5' : 'py-2.5',
        selectedTaskId === task.id
          ? 'border-seal/35 bg-seal/6'
          : 'border-transparent hover:border-line hover:bg-surface-2/70',
      )}
    >
      {sortable && !multiSelect ? (
        <span
          draggable
          onDragStart={(e) => {
            // 与整行拖拽区分开：手柄只表达「排序」，不携带跨清单移动语义。
            e.stopPropagation()
            e.dataTransfer.setData(SORT_MIME, String(task.id))
            e.dataTransfer.effectAllowed = 'move'
            onSortStart?.(task.id)
          }}
          onDragEnd={() => onSortEnd?.()}
          onClick={(e) => e.stopPropagation()}
          title="拖动调整顺序"
          className="mt-[2px] cursor-grab text-ink-3/60 opacity-0 transition-opacity hover:text-ink-2 group-hover/row:opacity-100 active:cursor-grabbing"
        >
          <IconGrip size={13} />
        </span>
      ) : null}

      {multiSelect ? (
        <div className="pt-[3px]">
          <Checkbox checked={selectedIds.includes(task.id)} onChange={() => toggleSelected(task.id)} />
        </div>
      ) : (
        <div className="pt-[1px]">
          <RoundCheck
            checked={done}
            color={task.priority === 3 ? 'var(--p-high)' : undefined}
            onChange={() => void toggleTask(task.id)}
          />
        </div>
      )}

      <div className="min-w-0 flex-1">
        <div className="flex items-start gap-1.5">
          <div className="flex min-w-0 flex-1 items-center gap-1.5">
            <PriorityFlag priority={task.priority} />
            {task.pinned ? (
              <span title="已置顶" className="inline-flex shrink-0 text-seal">
                <IconPin size={12} />
              </span>
            ) : null}
            {task.starred ? (
              <span title="已收藏" className="inline-flex shrink-0 text-[var(--p-mid)]">
                <IconStar size={12} />
              </span>
            ) : null}
            {editing ? (
              <input
                ref={inputRef}
                {...compositionProps}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onBlur={() => void commit()}
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => {
                  if (isComposing(e)) return
                  if (e.key === 'Enter') void commit()
                  if (e.key === 'Escape') {
                    setDraft(task.title)
                    setEditing(false)
                  }
                }}
                className="min-w-0 flex-1 rounded border border-seal/40 bg-surface px-1.5 py-0.5 text-[0.84375rem] outline-none"
              />
            ) : (
              <span
                onDoubleClick={(e) => {
                  e.stopPropagation()
                  setEditing(true)
                }}
                className={cx(
                  'min-w-0 break-words text-[0.84375rem] leading-6',
                  done ? 'text-ink-3 line-through decoration-ink-3/50' : 'text-ink',
                )}
              >
                {task.title}
              </span>
            )}
          </div>

          <div className="flex shrink-0 items-center opacity-0 transition-opacity group-hover/row:opacity-100">
            <IconButton
              icon={IconPin}
              label={task.pinned ? '取消置顶' : '置顶'}
              size={13}
              onClick={(e) => {
                e.stopPropagation()
                void updateTask(task.id, { pinned: !task.pinned })
              }}
            />
            <IconButton
              icon={IconStar}
              label={task.starred ? '取消收藏' : '收藏'}
              size={13}
              onClick={(e) => {
                e.stopPropagation()
                void updateTask(task.id, { starred: !task.starred })
              }}
            />
            <IconButton
              icon={IconPencil}
              label="重命名"
              size={13}
              onClick={(e) => {
                e.stopPropagation()
                setEditing(true)
              }}
            />
            <div className="relative">
              <IconButton
                icon={IconMore}
                label="更多"
                size={13}
                onClick={(e) => {
                  e.stopPropagation()
                  setMenuOpen((v) => !v)
                }}
              />
              <Popover open={menuOpen} onClose={() => setMenuOpen(false)} align="right" width={170}>
                <RowMenuItems task={task} onClose={() => setMenuOpen(false)} onOpen={() => onOpen(task)} onDelete={() => void deleteTask(task.id)} />
              </Popover>
            </div>
          </div>
        </div>

        {showList && task.listName ? (
          <div className="mt-0.5 text-[0.71875rem] text-ink-3">{task.listName}</div>
        ) : null}

        {done ? (
          <div className="mt-0.5 text-[0.71875rem] text-ink-3">完成于 {relativeTime(task.completedAt)}</div>
        ) : (
          <TaskMeta task={task} />
        )}
      </div>
    </div>
  )
}

function RowMenuItems({
  task,
  onClose,
  onOpen,
  onDelete,
}: {
  task: Task
  onClose: () => void
  onOpen: () => void
  onDelete: () => void
}) {
  const { updateTask, confirm, moveTask, skipTask, duplicateTask } = useStore()
  const today = todayStr()

  return (
    <>
      <div className="px-2.5 py-1.5 text-[0.75rem] text-ink-3">优先级</div>
      <div className="flex gap-1 px-1.5 pb-1.5">
        {([0, 1, 2, 3] as Priority[]).map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => {
              void updateTask(task.id, { priority: p })
              onClose()
            }}
            className={cx(
              'flex-1 rounded-md py-1 text-[0.75rem] transition-colors',
              task.priority === p ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:bg-surface-2',
            )}
          >
            {['无', '低', '中', '高'][p]}
          </button>
        ))}
      </div>
      <div className="my-1 border-t border-line" />
      <RowAction
        icon={IconClock}
        label="今天"
        onClick={() => {
          void moveTask(task.id, { dueDate: today })
          onClose()
        }}
      />
      <RowAction
        icon={IconClock}
        label="明天"
        onClick={() => {
          void moveTask(task.id, { dueDate: addDays(today, 1) })
          onClose()
        }}
      />
      <RowAction
        icon={IconX}
        label="清除日期"
        onClick={() => {
          void moveTask(task.id, { dueDate: null })
          onClose()
        }}
      />
      <RowAction
        icon={IconSparkle}
        label="切换重要 / 紧急"
        onClick={() => {
          void updateTask(task.id, { important: !task.important, urgent: !task.urgent })
          onClose()
        }}
      />
      <RowAction
        icon={IconPin}
        label={task.pinned ? '取消置顶' : '置顶'}
        onClick={() => {
          void updateTask(task.id, { pinned: !task.pinned })
          onClose()
        }}
      />
      <RowAction
        icon={IconStar}
        label={task.starred ? '取消收藏' : '收藏'}
        onClick={() => {
          void updateTask(task.id, { starred: !task.starred })
          onClose()
        }}
      />
      <RowAction
        icon={IconCopy}
        label="复制一份"
        onClick={() => {
          void duplicateTask(task.id)
          onClose()
        }}
      />
      <RowAction
        icon={IconPencil}
        label="编辑详情"
        onClick={() => {
          onOpen()
          onClose()
        }}
      />
      {/* 重复任务才有「跳过本次」：这次不做，日期直接推到下一次 */}
      {task.repeatRule && task.status !== 'done' ? (
        <RowAction
          icon={IconRepeat}
          label="跳过本次"
          onClick={() => {
            void skipTask(task.id)
            onClose()
          }}
        />
      ) : null}
      <div className="my-1 border-t border-line" />
      <RowAction
        icon={IconTrash}
        label="删除"
        danger
        onClick={async () => {
          const ok = await confirm({
            title: `删除「${task.title}」`,
            message: '删除后可在左下角撤销，超过 10 分钟才彻底消失。',
            confirmText: '删除',
            danger: true,
          })
          onClose()
          if (ok) onDelete()
        }}
      />
    </>
  )
}

function RowAction({
  icon: Icon,
  label,
  onClick,
  danger,
}: {
  icon: IconCmp
  label: string
  onClick: () => void
  danger?: boolean
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
      className={cx(
        'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] transition-colors',
        danger ? 'text-p-high hover:bg-p-high/10' : 'text-ink hover:bg-surface-2',
      )}
    >
      <Icon size={13} className={danger ? '' : 'text-ink-3'} />
      {label}
    </button>
  )
}

/* ---------------- 分组 ---------------- */

interface Bucket {
  key: string
  label: string
  tone?: 'danger' | 'accent' | 'normal'
  tasks: Task[]
}

const BUCKET_DEFS: { key: string; label: string; tone?: Bucket['tone'] }[] = [
  { key: 'overdue', label: '逾期', tone: 'danger' },
  { key: 'today', label: '今天', tone: 'accent' },
  { key: 'tomorrow', label: '明天' },
  { key: 'soon', label: '本周内' },
  { key: 'later', label: '更远' },
  { key: 'nodate', label: '无日期' },
]

export function bucketize(tasks: Task[]): Bucket[] {
  const today = todayStr()
  const map = new Map<string, Task[]>()
  for (const t of tasks) {
    let key: string
    if (t.status === 'done') key = 'done'
    else if (!t.dueDate) key = 'nodate'
    else {
      const diff = dayDiff(t.dueDate, today)
      if (diff < 0) key = 'overdue'
      else if (diff === 0) key = 'today'
      else if (diff === 1) key = 'tomorrow'
      else if (diff <= 7) key = 'soon'
      else key = 'later'
    }
    const arr = map.get(key)
    if (arr) arr.push(t)
    else map.set(key, [t])
  }

  const order = [...BUCKET_DEFS, { key: 'done', label: '已完成' }]
  const out: Bucket[] = []
  for (const def of order) {
    const list = map.get(def.key)
    if (list?.length) out.push({ key: def.key, label: def.label, tone: def.tone, tasks: list })
  }
  return out
}

/* ---------------- 快速添加 ---------------- */

export function QuickAdd({ autoFocus, placeholder }: { autoFocus?: boolean; placeholder?: string }) {
  const { createTask, ensureTags, lists, selection } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()
  const [text, setText] = useState('')
  const [expanded, setExpanded] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const parsed = useMemo(
    () => parseQuickAdd(text, { lists: lists.map((l) => ({ id: l.id, name: l.name })) }),
    [text, lists],
  )

  const submit = async () => {
    const title = parsed.title.trim()
    if (!title) return
    let tagIds: number[] | undefined
    if (parsed.tagNames.length) {
      const created = await ensureTags(parsed.tagNames)
      tagIds = created.map((t) => t.id)
    }
    await createTask({
      title,
      dueDate: parsed.dueDate,
      dueTime: parsed.dueTime,
      repeatRule: parsed.repeatRule,
      ...(parsed.priority !== null ? { priority: parsed.priority } : {}),
      ...(parsed.important !== null ? { important: parsed.important } : {}),
      ...(parsed.urgent !== null ? { urgent: parsed.urgent } : {}),
      ...(parsed.listId !== null ? { listId: parsed.listId } : {}),
      ...(tagIds ? { tagIds } : {}),
    })
    setText('')
    inputRef.current?.focus()
  }

  const chipIcon: Record<Chip['kind'], IconCmp> = {
    date: IconClock,
    time: IconClock,
    repeat: IconRepeat,
    priority: IconFlag,
    tag: IconTag,
    list: IconList,
    quadrant: IconSparkle,
  }

  const targetName = selection.kind === 'list' ? (lists.find((l) => l.id === selection.id)?.name ?? '清单') : '收集箱'

  return (
    <div className="relative">
      <div
        className={cx(
          'flex items-center gap-2 rounded-xl border bg-surface px-3 py-2 transition-all',
          expanded ? 'border-seal/45 shadow-[var(--shadow-sm)]' : 'border-line hover:border-line-strong',
        )}
      >
        <IconPlus size={15} className="shrink-0 text-ink-3" />
        <input
          ref={inputRef}
          id="shenshi-quickadd"
          autoFocus={autoFocus}
          {...compositionProps}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onFocus={() => setExpanded(true)}
          onBlur={() => window.setTimeout(() => setExpanded(false), 160)}
          onKeyDown={(e) => {
            // 输入法组合期间（选词/取消候选），Enter 与 Esc 属于 IME，不触发提交或清空。
            if (isComposing(e)) return
            if (e.key === 'Enter') {
              e.preventDefault()
              void submit()
            }
            if (e.key === 'Escape') {
              setText('')
              inputRef.current?.blur()
            }
          }}
          placeholder={placeholder ?? '记下一件事… 试试「明天下午3点交材料 #工作 !高」'}
          className="min-w-0 flex-1 bg-transparent text-[0.84375rem] outline-none placeholder:text-ink-3"
        />
        {text ? (
          <button
            type="button"
            onClick={() => setText('')}
            className="shrink-0 text-ink-3 transition-colors hover:text-ink"
            aria-label="清空"
          >
            <IconX size={14} />
          </button>
        ) : (
          <kbd className="hidden shrink-0 rounded border border-line bg-surface-2 px-1.5 py-0.5 font-mono text-[0.625rem] text-ink-3 sm:block">
            Enter
          </kbd>
        )}
      </div>

      {/* 识别结果预览 */}
      {text && (parsed.chips.length > 0 || parsed.title) ? (
        <div className="mt-1.5 flex flex-wrap items-center gap-1.5 px-1">
          {parsed.chips.map((c, i) => {
            const Icon = chipIcon[c.kind]
            return (
              <span
                key={`${c.kind}-${i}`}
                className="inline-flex items-center gap-1 rounded-md border border-seal/30 bg-seal/8 px-1.5 py-0.5 text-[0.71875rem] text-seal"
              >
                <Icon size={11} />
                {c.label}
              </span>
            )
          })}
          {parsed.title ? (
            <span className="text-[0.71875rem] text-ink-3">
              标题：{parsed.title}
            </span>
          ) : (
            <span className="text-[0.71875rem] text-ink-3">还差一个标题</span>
          )}
          <span className="ml-auto hidden text-[0.6875rem] text-ink-3 sm:block">归入「{targetName}」</span>
        </div>
      ) : null}
    </div>
  )
}

/* ---------------- 列表视图 ---------------- */

export function TaskListView({
  tasks,
  onOpen,
  emptyKey,
  showQuickAdd = true,
}: {
  tasks: Task[]
  onOpen: (t: Task) => void
  emptyKey: keyof typeof QUOTES | string
  showQuickAdd?: boolean
}) {
  const {
    multiSelect,
    selectedIds,
    clearSelected,
    batch,
    lists,
    sortBy,
    reorderTasks,
    selection,
    purgeCompleted,
    confirm,
  } = useStore()
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const buckets = useMemo(() => bucketize(tasks), [tasks])

  /** 清空已完成：在清单内就只清这个清单，在智能视图里则清全部。 */
  const purgeDone = async () => {
    const listId = selection.kind === 'list' ? selection.id : undefined
    const ok = await confirm({
      title: '清空已完成任务',
      message: listId
        ? '将删除当前清单里所有已完成的任务。删除记录会留在操作历史中。'
        : '将删除全部已完成的任务。删除记录会留在操作历史中。',
      confirmText: '清空',
      danger: true,
    })
    if (ok) await purgeCompleted(listId)
  }

  // 手动排序仅在「手动」模式下开放，避免与其它排序规则打架。
  const sortable = sortBy === 'manual' && !multiSelect
  const [dragId, setDragId] = useState<number | null>(null)
  const [dropAt, setDropAt] = useState<{ id: number; after: boolean } | null>(null)

  /** 松手落位：把拖动行插到目标行之前或之后，新顺序以整个可见列表为准。 */
  const commitSort = (bucketTasks: Task[], targetId: number, after: boolean) => {
    if (dragId === null || dragId === targetId) return
    if (!bucketTasks.some((t) => t.id === dragId)) return
    const ids = tasks.map((t) => t.id)
    const from = ids.indexOf(dragId)
    if (from < 0) return
    ids.splice(from, 1)
    // 移除拖动行后，目标下标会左移一位，必须重新定位。
    let to = ids.indexOf(targetId)
    if (to < 0) return
    if (after) to += 1
    ids.splice(to, 0, dragId)
    void reorderTasks(ids)
  }

  return (
    <div className="relative">
      {showQuickAdd ? (
        <div className="px-5 pt-1 pb-3">
          <QuickAdd placeholder="记下一件事… 试试「明天下午3点交材料 #工作 !高」" />
        </div>
      ) : null}

      <div className="px-5 pb-24">
        {buckets.length === 0 ? (
          <EmptyForView emptyKey={emptyKey} />
        ) : (          buckets.map((b) => (
            <section key={b.key} className="mb-1">
              <div className="mb-1 flex w-full items-center gap-2 px-3 pt-3">
                <button
                  type="button"
                  onClick={() => setCollapsed((prev) => ({ ...prev, [b.key]: !prev[b.key] }))}
                  className="flex flex-1 items-center gap-2 text-left"
                >
                  <span
                    className={cx(
                      'text-[0.75rem] font-semibold tracking-wide',
                      b.tone === 'danger' ? 'text-p-high' : b.tone === 'accent' ? 'text-seal' : 'text-ink-2',
                    )}
                  >
                    {b.label}
                  </span>
                  <span className="text-[0.6875rem] text-ink-3 tabular-nums">{b.tasks.length}</span>
                  {b.tone === 'danger' ? (
                    <span className="text-[0.6875rem] text-ink-3">· 慎终如始，则无败事</span>
                  ) : null}
                </button>
                {b.key === 'done' ? (
                  <button
                    type="button"
                    data-purge-completed
                    onClick={() => void purgeDone()}
                    title="删除已完成任务，只留下历史记录"
                    className="rounded-md px-1.5 py-0.5 text-[0.6875rem] text-ink-3 transition-colors hover:bg-p-high/10 hover:text-p-high"
                  >
                    清空
                  </button>
                ) : null}
              </div>
              {!collapsed[b.key] ? (
                <div className="space-y-0.5">
                  {b.tasks.map((t) => (
                    <div
                      key={t.id}
                      onDragOver={(e) => {
                        if (!sortable || dragId === null) return
                        e.preventDefault()
                        e.dataTransfer.dropEffect = 'move'
                        const r = e.currentTarget.getBoundingClientRect()
                        setDropAt({ id: t.id, after: e.clientY > r.top + r.height / 2 })
                      }}
                      onDragLeave={(e) => {
                        if (e.currentTarget.contains(e.relatedTarget as Node)) return
                        setDropAt((prev) => (prev?.id === t.id ? null : prev))
                      }}
                      onDrop={(e) => {
                        if (!sortable || dragId === null) return
                        e.preventDefault()
                        // 落点在这里现算，不依赖 dropAt —— 它可能还停在上一帧的判定上。
                        const r = e.currentTarget.getBoundingClientRect()
                        const after = e.clientY > r.top + r.height / 2
                        // 落点只在「同一分区内」有意义：跨分区拖动不改变日期与优先级，本就不该允许。
                        commitSort(b.tasks, t.id, after)
                        setDragId(null)
                        setDropAt(null)
                      }}
                      className={cx(
                        'rounded-xl border-t-2 border-b-2',
                        dropAt?.id === t.id && dropAt.after
                          ? 'border-t-transparent border-b-seal'
                          : dropAt?.id === t.id
                            ? 'border-t-seal border-b-transparent'
                            : 'border-transparent',
                      )}
                    >
                      <TaskRow
                        task={t}
                        onOpen={onOpen}
                        showList={b.key === 'done'}
                        sortable={sortable}
                        onSortStart={(id) => setDragId(id)}
                        onSortEnd={() => {
                          setDragId(null)
                          setDropAt(null)
                        }}
                      />
                    </div>
                  ))}
                </div>
              ) : null}
            </section>
          ))
        )}
      </div>

      {/* 多选操作条 */}
      {multiSelect && selectedIds.length > 0 ? (
        <div className="pointer-events-none fixed bottom-6 left-1/2 z-30 -translate-x-1/2">
          <div className="pointer-events-auto flex items-center gap-2 rounded-2xl border border-line bg-surface/95 px-3 py-2 shadow-[var(--shadow-lg)] backdrop-blur">
            <span className="px-1 text-[0.78125rem] text-ink-2">已选 {selectedIds.length} 项</span>
            <Button variant="primary" size="sm" icon={IconCheck} onClick={() => void batch('complete')}>
              完成
            </Button>
            <Button variant="outline" size="sm" icon={IconCircle} onClick={() => void batch('reopen')}>
              恢复
            </Button>
            <Button
              variant="outline"
              size="sm"
              icon={IconMove}
              onClick={() => {
                const today = todayStr()
                void batch('move', { dueDate: today })
              }}
            >
              移到今天
            </Button>
            <select
              className="h-7 rounded-lg border border-line bg-surface px-1.5 text-[0.75rem]"
              defaultValue=""
              onChange={(e) => {
                if (!e.target.value) return
                void batch('move', { listId: Number(e.target.value) })
                e.currentTarget.value = ''
              }}
            >
              <option value="">移到清单…</option>
              {lists.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
            <Button variant="outline" size="sm" icon={IconPin} onClick={() => void batch('pin')}>
              置顶
            </Button>
            <Button variant="outline" size="sm" icon={IconStar} onClick={() => void batch('star')}>
              收藏
            </Button>
            <Button
              variant="ghost"
              size="sm"
              icon={IconTrash}
              className="text-p-high"
              onClick={async () => {
                const ok = await confirm({
                  title: `删除已选的 ${selectedIds.length} 项`,
                  message: '删除后可在左下角撤销，超过 10 分钟才彻底消失。',
                  confirmText: '删除',
                  danger: true,
                })
                if (ok) await batch('delete')
              }}
            >
              删除
            </Button>
            <IconButton icon={IconX} label="取消选择" onClick={clearSelected} />
          </div>
        </div>
      ) : null}
    </div>
  )
}

/* ---------------- 空状态 ---------------- */

const EMPTY_HINT: Record<string, string> = {
  inbox: '把脑子里的事先写下来，归属以后再定。',
  today: '今日无事，或可添一件真正要紧的。',
  tomorrow: '明天尚无安排。',
  week: '本周剩下的日子是空的。',
  next7: '未来七天还没有安排。',
  overdue: '没有逾期事项，节奏保持得不错。',
  nodate: '所有任务都已经排上了时间。',
  high: '还没有标为高优先级的任务。',
  starred: '点任务行右侧的星标，就能把它收到这里。',
  updated: '最近没有改动过任何任务。',
  recentdone: '这段时间还没有完成过什么。',
  all: '还没有任何任务。',
  done: '完成的任务会在这里留档。',
  list: '这个清单还是空的，从上面添加第一件事。',
  folder: '分组内还没有任务。',
  tag: '该标签下暂无任务。',
  search: '没有匹配的任务，换个关键词试试。',
  calendar: '这一段时间没有安排。',
  board: '这一列还是空的，把任务拖进来即可。',
  table: '这张表还没有内容。',
}

export function EmptyForView({ emptyKey }: { emptyKey: keyof typeof QUOTES | string }) {
  const quote = QUOTES[emptyKey as keyof typeof QUOTES] ?? QUOTES.all
  return (
    <EmptyState
      text={quote.text}
      source={quote.source}
      hint={EMPTY_HINT[emptyKey as string] ?? EMPTY_HINT.all}
    />
  )
}

export function SearchBar() {
  const { keyword, setKeyword, refreshTasks } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()
  return (
    <div className="relative flex h-8 items-center gap-2 rounded-lg border border-line bg-surface px-2.5 focus-within:border-seal/50">
      <IconSearch size={14} className="text-ink-3" />
      <input
        id="shenshi-search"
        {...compositionProps}
        value={keyword}
        onChange={(e) => setKeyword(e.target.value)}
        onKeyDown={(e) => {
          if (isComposing(e)) return
          if (e.key === 'Escape') {
            setKeyword('')
            e.currentTarget.blur()
          }
        }}
        placeholder="搜索任务与备注"
        className="w-40 bg-transparent text-[0.8125rem] outline-none transition-all placeholder:text-ink-3 focus:w-56"
      />
      {keyword ? (
        <button
          type="button"
          aria-label="清空搜索"
          className="text-ink-3 hover:text-ink"
          onClick={() => {
            setKeyword('')
            void refreshTasks()
          }}
        >
          <IconX size={13} />
        </button>
      ) : null}
    </div>
  )
}
