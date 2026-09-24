import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type SyntheticEvent,
} from 'react'

import { dayDiff, dueLabel, isOverdue, relativeTime, todayStr, addDays } from '../lib/date'
import { QUOTES } from '../lib/quotes'
import { parseQuickAdd, describeRepeat, QUICK_ADD_HINTS, type Chip } from '../lib/nlp'
import { useIMEGuard } from '../lib/ime'
import { useStore } from '../store/AppStore'
import type { Folder, List, Priority, Selection, Tag, Task, TaskPatch } from '../types'
import {
  IconBell,
  IconCheck,
  IconCircle,
  IconClock,
  IconCopy,
  IconFlag,
  IconGrip,
  IconLink,
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
      {task.estimateMinutes > 0 ? (
        <span title="预计时长" className="inline-flex items-center gap-1 text-[0.71875rem] text-ink-3 tabular-nums">
          <IconClock size={11.5} />
          {task.estimateMinutes >= 60 && task.estimateMinutes % 60 === 0 ? `${task.estimateMinutes / 60}时` : `${task.estimateMinutes}分`}
        </span>
      ) : null}
      {task.progress > 0 ? (
        <span
          title="进度"
          className={cx('inline-flex items-center gap-1 text-[0.71875rem] tabular-nums', task.progress >= 100 ? 'text-jade' : 'text-ink-3')}
        >
          {task.progress}%
        </span>
      ) : null}
      {task.links?.some((l) => l.kind === 'blocked_by' && l.status !== 'done') ? (
        <span title="有未完成的依赖" className="inline-flex items-center gap-1 text-[0.71875rem] font-medium text-p-high">
          <IconLink size={11.5} />
          被依赖阻塞
        </span>
      ) : null}
      {task.status === 'in_progress' ? (
        <span title="进行中" className="inline-flex items-center rounded-full bg-jade/12 px-1.5 text-[0.65625rem] font-medium text-jade">
          进行中
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
  const { toggleTask, updateTask, deleteTask, multiSelect, selectedIds, toggleSelected, selectedTaskId, toast } =
    useStore()
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
    if (!value) {
      // 空标题没有意义。原先只是静默还原，用户看到的是「改了但没生效」；
      // 这里明确说一句，并把输入框内容恢复成原标题。
      if (draft !== task.title) toast('标题不能为空，已还原', 'info')
      setDraft(task.title)
      return
    }
    if (value !== task.title) await updateTask(task.id, { title: value })
  }

  const onDragStart = (e: DragEvent<HTMLDivElement>) => {
    e.dataTransfer.setData(DRAG_MIME, String(task.id))
    e.dataTransfer.setData('text/plain', task.title)
    e.dataTransfer.effectAllowed = 'move'
  }

  return (
    <div
      data-task-row={task.id}
      role="button"
      tabIndex={0}
      aria-label={`${task.title}${done ? '（已完成）' : ''}`}
      draggable={draggable && !editing}
      onDragStart={onDragStart}
      onClick={() => (multiSelect ? toggleSelected(task.id) : onOpen(task))}
      onKeyDown={(e) => {
        // 只处理落在行本身上的按键；子控件（勾选框、操作按钮）的按键由它们自己处理。
        if (e.target !== e.currentTarget) return
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          if (multiSelect) toggleSelected(task.id)
          else onOpen(task)
          return
        }
        // 双击改名对键盘不可达，补一个 F2（与文件管理器、任务类工具的习惯一致）
        if (e.key === 'F2') {
          e.preventDefault()
          setEditing(true)
        }
      }}
      className={cx(
        'group/row relative flex cursor-pointer items-start gap-2.5 rounded-xl border px-3 transition-colors',
        dense ? 'py-1.5' : 'py-2.5',
        multiSelect && selectedIds.includes(task.id)
          ? 'border-seal/45 bg-seal/10'
          : selectedTaskId === task.id
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
                title="双击改名（键盘：F2）"
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
                // 可访问名带上任务标题，读屏逐行浏览时能分辨是哪个任务的菜单；
                // title 保持「更多」不变 —— 冒烟脚本按 button[title="更多"] 定位。
                aria-label={`「${task.title}」的更多操作`}
                aria-haspopup="true"
                aria-expanded={menuOpen}
                onClick={(e) => {
                  e.stopPropagation()
                  setMenuOpen((v) => !v)
                }}
                onKeyDown={(e) => {
                  // 选单约定的「↓ 打开并落到第一项」；其余按键交给全局处理
                  if (e.key === 'ArrowDown') {
                    e.preventDefault()
                    setMenuOpen(true)
                  }
                }}
              />
              <Popover open={menuOpen} onClose={() => setMenuOpen(false)} align="right" width={200} menu>
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
  const { updateTask, confirm, moveTask, skipTask, duplicateTask, lists, folders } = useStore()
  const today = todayStr()
  const menuRef = useRef<HTMLDivElement>(null)

  /** ↑↓ 在菜单项之间移动焦点（标准选单的键盘约定），省得一路 Tab 穿过十几项。 */
  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
    if (items.length === 0) return
    e.preventDefault()
    const i = items.indexOf(document.activeElement as HTMLElement)
    const next = e.key === 'ArrowDown' ? (i + 1) % items.length : i <= 0 ? items.length - 1 : i - 1
    items[next]?.focus()
  }

  const folderName = (l: List) => (l.folderId == null ? undefined : folders.find((f) => f.id === l.folderId)?.name)

  return (
    <div ref={menuRef} role="menu" aria-label={`「${task.title}」的操作`} onKeyDown={onKeyDown}>
      <div className="px-2.5 py-1.5 text-[0.75rem] text-ink-3">优先级</div>
      <div role="group" aria-label="优先级" className="flex gap-1 px-1.5 pb-1.5">
        {([0, 1, 2, 3] as Priority[]).map((p) => (
          <button
            key={p}
            type="button"
            aria-pressed={task.priority === p}
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
      <div role="separator" className="my-1 border-t border-line" />
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
          // 清日期要一并清时刻，否则会留下"没有到期日、却还挂着 09:00"的脏数据，
          // 而且此后任何一次"设日期"都会让那个旧时刻悄然复活。
          // 后端 PATCH 是三态语义（未传的字段一律不动），联动必须由调用方显式表达。
          void moveTask(task.id, { dueDate: null, dueTime: null })
          onClose()
        }}
      />
      {/* 重要与紧急原先是同一条「一起翻转」：只想改一个也会连带另一个。拆成两条独立开关。 */}
      <RowAction
        icon={IconSparkle}
        label={task.important ? '取消「重要」' : '标为「重要」'}
        onClick={() => {
          void updateTask(task.id, { important: !task.important })
          onClose()
        }}
      />
      <RowAction
        icon={IconClock}
        label={task.urgent ? '取消「紧急」' : '标为「紧急」'}
        onClick={() => {
          void updateTask(task.id, { urgent: !task.urgent })
          onClose()
        }}
      />
      {/* 移动到其它清单：跨清单移动原先只能拖 —— 这里补的键盘等价入口 */}
      <div role="group" aria-label="移动到清单" className="px-1.5 pb-1.5">
        <div className="px-1 pb-1 text-[0.75rem] text-ink-3">移动到清单</div>
        <div className="max-h-36 overflow-y-auto">
          {lists.map((l) => (
            <button
              key={l.id}
              type="button"
              role="menuitem"
              aria-current={l.id === task.listId ? 'true' : undefined}
              onClick={() => {
                if (l.id !== task.listId) void moveTask(task.id, { listId: l.id })
                onClose()
              }}
              className={cx(
                'flex w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-[0.78125rem] transition-colors',
                l.id === task.listId ? 'bg-seal/10 text-seal' : 'text-ink-2 hover:bg-surface-2',
              )}
            >
              <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: l.color }} />
              <span className="flex-1 truncate">{l.name}</span>
              {folderName(l) ? <span className="shrink-0 text-[0.6875rem] text-ink-3">{folderName(l)}</span> : null}
              {l.id === task.listId ? <IconCheck size={12} className="shrink-0" /> : null}
            </button>
          ))}
        </div>
      </div>
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
        label="复制"
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
      <div role="separator" className="my-1 border-t border-line" />
      <RowAction
        icon={IconTrash}
        label="删除"
        danger
        onClick={async () => {
          const ok = await confirm({
            title: `删除「${task.title}」`,
            message: '删除后可在底部状态栏撤销，超过 10 分钟才彻底消失。',
            confirmText: '删除',
            danger: true,
          })
          onClose()
          if (ok) onDelete()
        }}
      />
    </div>
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
      role="menuitem"
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

/* ---------------- 快速添加的符号补全 ---------------- */

type Sug =
  | { kind: 'tag'; key: string; value: string; label: string; color: string }
  | { kind: 'newTag'; key: string; value: string; label: string }
  | { kind: 'priority'; key: string; value: string; label: string; priority: number; desc: string }
  | { kind: 'list'; key: string; value: string; label: string; color: string; folderName?: string }

/** 全角触发符归一化：中文输入法下 # ! / 常被输入成 ＃ ！ ／，两者等价。 */
const FULLWIDTH_TRIGGER: Record<string, string> = { '＃': '#', '！': '!', '／': '/' }

/** 检测光标是否正处于某个符号触发的补全语境里，返回触发符与已输入的查询串。 */
function detectToken(
  text: string,
  caret: number,
): { trigger: string; query: string; start: number; end: number } | null {
  if (!text) return null
  let i = Math.min(caret, text.length) - 1
  let query = ''
  while (i >= 0) {
    const ch = text[i]
    const trigger = ch === '#' || ch === '!' || ch === '/' ? ch : FULLWIDTH_TRIGGER[ch]
    if (trigger) {
      const prev = i > 0 ? text[i - 1] : ''
      // 触发符前必须是行首或空白，避免误伤「C#」「http://」这类正文。
      if (i === 0 || /\s/.test(prev)) return { trigger, query, start: i, end: Math.min(caret, text.length) }
      return null
    }
    if (/\s/.test(ch)) return null // 空白终止当前 token
    query = ch + query
    i--
  }
  return null
}

const PRIORITY_SUGS = [
  { value: '高', priority: 3, desc: '最高优先级' },
  { value: '中', priority: 2, desc: '中优先级' },
  { value: '低', priority: 1, desc: '低优先级' },
]

function buildSuggestions(
  token: { trigger: string; query: string } | null,
  tags: Tag[],
  lists: List[],
  folders: Folder[],
): Sug[] {
  if (!token) return []
  const q = token.query.toLowerCase()
  if (token.trigger === '#') {
    const out: Sug[] = tags
      .filter((t) => t.name.toLowerCase().includes(q))
      .slice(0, 8)
      .map((t) => ({ kind: 'tag', key: `tag-${t.id}`, value: t.name, label: t.name, color: t.color }))
    const exact = tags.some((t) => t.name.toLowerCase() === q)
    if (token.query && !exact) out.push({ kind: 'newTag', key: 'newtag', value: token.query, label: token.query })
    return out
  }
  if (token.trigger === '!') {
    return PRIORITY_SUGS.filter((p) => p.value.toLowerCase().includes(q) || q === '').map((p) => ({
      kind: 'priority',
      key: `pri-${p.priority}`,
      value: p.value,
      label: p.value,
      priority: p.priority,
      desc: p.desc,
    }))
  }
  if (token.trigger === '/') {
    const folderName = (l: List) => (l.folderId == null ? undefined : folders.find((f) => f.id === l.folderId)?.name)
    return lists
      .filter((l) => l.name.toLowerCase().includes(q))
      .slice(0, 8)
      .map((l) => ({
        kind: 'list',
        key: `list-${l.id}`,
        value: l.name,
        label: l.name,
        color: l.color,
        folderName: folderName(l),
      }))
  }
  return []
}

/* ---------------- 快速添加 ---------------- */

export function QuickAdd({
  autoFocus,
  placeholder,
  defaults,
}: {
  autoFocus?: boolean
  placeholder?: string
  /** 预填字段（列头新建用）。标题解析出的显式条件优先于 defaults。 */
  defaults?: TaskPatch
}) {
  const { createTask, ensureTags, lists, tags, folders, selection, toast } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()
  const [text, setText] = useState('')
  const [expanded, setExpanded] = useState(false)
  const [caret, setCaret] = useState(0)
  const [activeIndex, setActiveIndex] = useState(0)
  const [suppress, setSuppress] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const pendingCaret = useRef<number | null>(null)

  const parsed = useMemo(
    () => parseQuickAdd(text, { lists: lists.map((l) => ({ id: l.id, name: l.name })) }),
    [text, lists],
  )

  // 当前光标处是否处于符号补全语境（# 标签 / ! 优先级 / / 清单）。
  const token = useMemo(() => detectToken(text, caret), [text, caret])
  const tokenKey = token ? `${token.start}:${token.trigger}:${token.query}` : ''
  const suggestions = useMemo(
    () => buildSuggestions(token, tags, lists, folders),
    [token, tags, lists, folders],
  )
  const showSuggest = token !== null && suggestions.length > 0 && !suppress

  // 每次进入新的补全语境都回到首项并解除抑制。
  useEffect(() => {
    setActiveIndex(0)
    setSuppress(false)
  }, [tokenKey])

  // 程序化改写文本后，把光标复位到插入点。
  useEffect(() => {
    if (pendingCaret.current != null && inputRef.current) {
      inputRef.current.setSelectionRange(pendingCaret.current, pendingCaret.current)
      pendingCaret.current = null
    }
  }, [text])

  const tokenRef = useRef(token)
  tokenRef.current = token

  const syncCaret = (e: SyntheticEvent<HTMLInputElement>) => {
    // apply 已程序化移动光标并写入 pendingCaret，跳过紧随的 keyup/click 同步，避免回写脏值。
    if (pendingCaret.current != null) return
    const pos = e.currentTarget.selectionStart
    if (pos != null) setCaret(pos)
  }

  const apply = (item: Sug) => {
    const t = tokenRef.current
    if (!t) return
    const before = text.slice(0, t.start)
    const after = text.slice(t.end)
    let insert = ''
    if (item.kind === 'priority') insert = `!${item.value} `
    else if (item.kind === 'tag' || item.kind === 'newTag') insert = `#${item.value} `
    else if (item.kind === 'list') insert = `/${item.value} `
    const newCaret = before.length + insert.length
    setText(before + insert + after)
    setCaret(newCaret)
    pendingCaret.current = newCaret
    setSuppress(false)
    setActiveIndex(0)
    inputRef.current?.focus()
  }

  const submit = async () => {
    const title = parsed.title.trim()
    if (!title) {
      // 只敲了「#工作」这类记号、没写标题时，原先按回车毫无反应 —— 说清差什么。
      if (text.trim()) toast('还差一个标题', 'info')
      return
    }
    let tagIds: number[] | undefined
    if (parsed.tagNames.length) {
      const created = await ensureTags(parsed.tagNames)
      if (created === null) return // 标签没建成：中止建任务，保留输入（错误已 toast）
      tagIds = created.map((t) => t.id)
    }
    // 智能视图兜底日期：在「今天/明天」清单里记的事没写日期就落收集箱、
    // 视图里再也看不见；标题解析与 defaults 都没给时补一个。nodate/其它键不注入。
    const smartDue =
      parsed.dueDate || defaults?.dueDate
        ? undefined
        : selection.kind === 'smart' && selection.key === 'today'
          ? todayStr()
          : selection.kind === 'smart' && selection.key === 'tomorrow'
            ? addDays(todayStr(), 1)
            : undefined
    const t = await createTask({
      ...defaults,
      ...(smartDue ? { dueDate: smartDue } : {}),
      title,
      ...(parsed.dueDate ? { dueDate: parsed.dueDate } : {}),
      ...(parsed.dueTime ? { dueTime: parsed.dueTime } : {}),
      ...(parsed.repeatRule ? { repeatRule: parsed.repeatRule } : {}),
      ...(parsed.priority !== null ? { priority: parsed.priority } : {}),
      ...(parsed.important !== null ? { important: parsed.important } : {}),
      ...(parsed.urgent !== null ? { urgent: parsed.urgent } : {}),
      ...(parsed.listId !== null ? { listId: parsed.listId } : {}),
      ...(tagIds ? { tagIds } : {}),
    })
    if (!t) return // 创建失败：保留输入与光标，别让用户重敲一遍
    setText('')
    setCaret(0)
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
          onChange={(e) => {
            setText(e.target.value)
            syncCaret(e)
          }}
          onFocus={() => setExpanded(true)}
          onBlur={() => window.setTimeout(() => setExpanded(false), 160)}
          onKeyUp={(e) => syncCaret(e)}
          onClick={(e) => syncCaret(e)}
          onKeyDown={(e) => {
            // 输入法组合期间（选词/取消候选），Enter 与 Esc 属于 IME，不触发提交或清空。
            if (isComposing(e)) return
            if (showSuggest) {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setActiveIndex((i) => Math.min(i + 1, suggestions.length - 1))
                return
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault()
                setActiveIndex((i) => Math.max(i - 1, 0))
                return
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                // 查询已与某条建议完全一致：符号已经写完整了（如「!高」「#工作」），
                // Enter 的语义是「记下这件事」，而不是再补一个空格关掉弹层——
                // 否则用户要按两次 Enter 才能建任务。Tab 与部分匹配仍走补全。
                const q = tokenRef.current?.query.toLowerCase() ?? ''
                const complete =
                  e.key === 'Enter' && q !== '' && suggestions.some((s) => s.value.toLowerCase() === q)
                if (!complete) {
                  e.preventDefault()
                  const it = suggestions[activeIndex]
                  if (it) apply(it)
                  return
                }
              }
              if (e.key === 'Escape') {
                e.preventDefault()
                setSuppress(true)
                return
              }
            }
            if (e.key === 'Enter') {
              e.preventDefault()
              void submit()
            }
            if (e.key === 'Escape') {
              setText('')
              setCaret(0)
              inputRef.current?.blur()
            }
          }}
          placeholder={placeholder ?? '记下一件事… 试试「明天下午3点交材料 #工作 !高 /项目推进」'}
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

      {/* 空输入聚焦时的语法提示：把可用的符号语法直接摆出来，解决「不知道能输入哪些」 */}
      {expanded && !text ? (
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 px-1 text-[0.6875rem] text-ink-3">
          {QUICK_ADD_HINTS.filter((h) => /^[#/!@＃！／]/.test(h.syntax)).map((h) => (
            <span key={h.syntax} className="inline-flex items-center gap-1">
              <code className="rounded border border-line bg-surface-2 px-1 py-px font-mono text-[0.625rem] text-ink-2">{h.syntax}</code>
              <span>{h.desc}</span>
            </span>
          ))}
        </div>
      ) : null}

      {/* 符号触发的自动建议 */}
      {showSuggest ? (
        <Popover open={showSuggest} onClose={() => setSuppress(true)} align="left" side="bottom" width={320} className="max-w-[88vw]">
          <div className="max-h-64 overflow-y-auto py-1">
            {suggestions.map((s, i) => (
              <button
                key={s.key}
                type="button"
                onMouseEnter={() => setActiveIndex(i)}
                onMouseDown={(e) => {
                  e.preventDefault()
                  apply(s)
                }}
                className={cx(
                  'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.8125rem] transition-colors',
                  i === activeIndex ? 'bg-seal/10 text-seal' : 'text-ink hover:bg-surface-2',
                )}
              >
                {s.kind === 'tag' || s.kind === 'list' ? (
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: s.color }} />
                ) : s.kind === 'priority' ? (
                  <IconFlag
                    size={14}
                    className="shrink-0"
                    style={{ color: s.priority === 3 ? 'var(--p-high)' : s.priority === 2 ? 'var(--p-mid)' : 'var(--p-low)' }}
                  />
                ) : (
                  <IconPlus size={14} className="shrink-0 text-ink-3" />
                )}
                <span className="flex-1 truncate">
                  {s.kind === 'newTag' ? `#${s.label}` : s.kind === 'priority' ? `!${s.label}` : s.kind === 'list' ? `/${s.label}` : s.label}
                </span>
                {s.kind === 'priority' ? (
                  <span className="shrink-0 text-[0.6875rem] text-ink-3">{s.desc}</span>
                ) : s.kind === 'list' && s.folderName ? (
                  <span className="shrink-0 truncate text-[0.6875rem] text-ink-3">{s.folderName}</span>
                ) : s.kind === 'newTag' ? (
                  <span className="shrink-0 text-[0.6875rem] text-ink-3">新建标签</span>
                ) : null}
              </button>
            ))}
          </div>
        </Popover>
      ) : null}

      {/* 识别结果预览 */}
      {text && (parsed.chips.length > 0 || parsed.title) && !showSuggest ? (
        // 解析结果（标签/日期/归入哪张清单）过去只有视觉呈现，读屏用户完全不知道
        <div role="status" aria-live="polite" className="mt-1.5 flex flex-wrap items-center gap-1.5 px-1">
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
          <span className="ml-auto hidden text-[0.6875rem] text-ink-3 sm:block">放入「{targetName}」</span>
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
    sortBy,
    reorderTasks,
    selection,
    purgeCompleted,
    confirm,
  } = useStore()
  // 折叠状态持久化：原先切走视图再回来会全部展开，每次都要重新收起一遍。
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    try {
      const raw = localStorage.getItem('shenshi.collapsed.sections')
      const parsed = raw ? (JSON.parse(raw) as unknown) : null
      return parsed && typeof parsed === 'object' ? (parsed as Record<string, boolean>) : {}
    } catch {
      return {}
    }
  })
  useEffect(() => {
    try {
      localStorage.setItem('shenshi.collapsed.sections', JSON.stringify(collapsed))
    } catch {
      // 隐私模式 / 配额满时不可写，静默降级为「本次会话内有效」
    }
  }, [collapsed])
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
        <div className="px-3 pt-1 pb-3 md:px-5">
          <QuickAdd placeholder="记下一件事… 试试「明天下午3点交材料 #工作 !高」" />
        </div>
      ) : null}

      <div className="px-3 pb-24 md:px-5">
        {buckets.length === 0 ? (
          <EmptyForView emptyKey={emptyKey} />
        ) : (          buckets.map((b) => (
            <section key={b.key} className="mb-1">
              <div className="mb-1 flex w-full items-center gap-2 px-3 pt-3">
                <button
                  type="button"
                  aria-expanded={!collapsed[b.key]}
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
      <BatchBar />
    </div>
  )
}

/**
 * 多选批处理条：fixed 定位不占布局，由 App 统一渲染一份，
 * 列表/表格/看板/四象限/日历进入多选后都能用（原先后台挂在列表视图内部，
 * 其它视图开了多选也看不到操作入口）。
 */
/**
 * 把清单按分组组织成 optgroup。
 * 分组是树（子分组可有任意层级），所以必须递归 —— 只列根级会让子分组里的清单
 * 在这一处直接消失，和侧栏看到的层级对不上。
 */
function groupListsForSelect(tree: Folder[], lists: List[]): { label: string; items: List[] }[] {
  const out: { label: string; items: List[] }[] = []
  const walk = (nodes: Folder[], prefix: string) => {
    for (const f of nodes) {
      const path = prefix ? `${prefix} / ${f.name}` : f.name
      const items = lists.filter((l) => l.folderId === f.id)
      if (items.length > 0) out.push({ label: path, items })
      walk(f.children ?? [], path)
    }
  }
  walk(tree, '')
  const loose = lists.filter((l) => l.folderId == null)
  if (loose.length > 0) out.unshift({ label: '未分组', items: loose })
  return out
}

export function BatchBar() {
  const { multiSelect, selectedIds, clearSelected, batch, lists, folders, confirm } = useStore()
  if (!multiSelect || selectedIds.length === 0) return null
  const listGroups = groupListsForSelect(folders, lists)
  return (
    // 抬到底部两个固定浮层（专注指示条 / 提醒中心）之上，三者不再互相遮挡
    <div className="pointer-events-none fixed inset-x-3 bottom-20 z-30 flex justify-center">
      {/* 窄屏上操作条会横向溢出屏幕：改成可换行 + 限宽，宽屏观感不变 */}
      <div className="pointer-events-auto flex max-w-full flex-wrap items-center justify-center gap-2 rounded-2xl border border-line bg-surface/95 px-3 py-2 shadow-[var(--shadow-lg)] backdrop-blur">
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
          aria-label="移动到其它清单"
          className="h-7 rounded-lg border border-control-line bg-surface px-1.5 text-[0.75rem]"
          defaultValue=""
          onChange={(e) => {
            if (!e.target.value) return
            void batch('move', { listId: Number(e.target.value) })
            e.currentTarget.value = ''
          }}
        >
          <option value="">移到清单…</option>
          {listGroups.map((g) => (
            <optgroup key={g.label} label={g.label}>
              {g.items.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </optgroup>
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
              message: '删除后可在底部状态栏撤销，超过 10 分钟才彻底消失。',
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

export function SearchBar({ className }: { className?: string }) {
  const { keyword, setKeyword } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()
  // 本地草稿 + 250ms 防抖：每敲一个字就打一次请求会让搜索明显发顿，
  // 响应乱序时结果还会串台（store 侧另有请求序号兜底，这里是第一道闸）。
  const [draft, setDraft] = useState(keyword)
  const timer = useRef<number | null>(null)

  // 外部改动关键词（清空、切换清单、快捷键）时同步回输入框
  useEffect(() => {
    setDraft(keyword)
  }, [keyword])

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
    },
    [],
  )

  const queue = (value: string) => {
    if (timer.current !== null) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setKeyword(value), 250)
  }

  const clear = () => {
    if (timer.current !== null) window.clearTimeout(timer.current)
    setDraft('')
    setKeyword('')
  }

  return (
    <div
      className={cx(
        'relative flex h-8 items-center gap-2 rounded-lg border border-line bg-surface px-2.5 focus-within:border-seal/50',
        className,
      )}
    >
      <IconSearch size={14} className="shrink-0 text-ink-3" />
      <input
        id="shenshi-search"
        {...compositionProps}
        value={draft}
        aria-label="搜索任务与备注"
        onChange={(e) => {
          setDraft(e.target.value)
          // 输入法组合中先不提交：此刻的 value 还是拼音串
          if (!(e.nativeEvent as InputEvent).isComposing) queue(e.target.value)
        }}
        onKeyDown={(e) => {
          if (isComposing(e)) return
          if (e.key === 'Escape') {
            clear()
            e.currentTarget.blur()
          }
        }}
        placeholder="搜索任务与备注"
        // 移动端随容器伸展（工具栏第一行与筛选/排序按钮共行）；
        // 桌面端保持定宽 + 聚焦加宽的原有行为。
        className="min-w-0 flex-1 bg-transparent text-[0.8125rem] outline-none transition-all placeholder:text-ink-3 md:w-40 md:flex-none md:focus:w-56"
      />
      {draft ? (
        <button
          type="button"
          aria-label="清空搜索"
          className="text-ink-3 hover:text-ink"
          onClick={clear}
        >
          <IconX size={13} />
        </button>
      ) : null}
    </div>
  )
}
