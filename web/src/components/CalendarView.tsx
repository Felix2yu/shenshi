import { useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent as ReactMouseEvent } from 'react'

import { api } from '../api/client'
import {
  addDays,
  addMonths,
  dayDiff,
  fullDate,
  minutesToHM,
  monthMatrix,
  nowHM,
  pad2,
  todayStr,
  toMinutes,
  weekDays,
  weekdayHeaders,
} from '../lib/date'
import { applyFilter, type TaskFilter } from '../lib/filter'
import { useIMEGuard } from '../lib/ime'
import { useStore } from '../store/AppStore'
import type { Task } from '../types'
import { IconChevronLeft, IconChevronRight, IconClock, IconPlus, IconRepeat, IconX } from './icons'
import { DRAG_MIME, PriorityFlag } from './TaskViews'
import { IconButton, cx } from './ui'

type Mode = 'month' | 'week' | 'day'

/** 日视图时间轴：每小时的高度。52px 在 24 小时下约 1250px，滚动浏览正合适。 */
const HOUR_H = 52
const DAY_MINUTES = 24 * 60
/** 时间轴上的最小吸附粒度（分钟）与无结束时间时的默认时长。 */
const SNAP_MIN = 15
const DEFAULT_DURATION = 45
const PX_PER_MIN = HOUR_H / 60
const HOURS = Array.from({ length: 24 }, (_, i) => i)
const BLOCK_MIN_H = 26

const isTimeStr = (v: string | null): v is string => !!v && /^\d{2}:\d{2}$/.test(v)

/** 时间轴上的任务块布局：重叠的任务并排，互不重叠的各自占满整行。 */
function placeDay(rows: { task: Task; start: number; end: number }[]) {
  type Item = { task: Task; start: number; end: number; lane: number }
  const out: (Item & { lanes: number })[] = []
  let cluster: Item[] = []
  let clusterEnd = -1
  const flush = () => {
    const lanes = cluster.reduce((m, x) => Math.max(m, x.lane + 1), 0)
    for (const x of cluster) out.push({ ...x, lanes })
    cluster = []
    clusterEnd = -1
  }
  for (const r of rows) {
    if (cluster.length && r.start >= clusterEnd) flush()
    const taken = new Set(cluster.filter((c) => c.end > r.start).map((c) => c.lane))
    let lane = 0
    while (taken.has(lane)) lane++
    cluster.push({ ...r, lane })
    clusterEnd = Math.max(clusterEnd, r.end)
  }
  flush()
  return out
}

/** 日历视图自持数据：它需要的是「一段时间范围内的全部任务」，与侧边栏选择无关。 */
export function CalendarView({ onOpen, filter }: { onOpen: (t: Task) => void; filter: TaskFilter }) {
  const { moveTask, version, toast } = useStore()
  const [mode, setMode] = useState<Mode>('month')
  const [anchor, setAnchor] = useState(todayStr())
  const [items, setItems] = useState<Task[]>([])
  const [loading, setLoading] = useState(false)
  const [dropDay, setDropDay] = useState<string | null>(null)
  const [adding, setAdding] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [weekStart, setWeekStart] = useState<0 | 1>(1)

  const range = useMemo(() => {
    // 日视图只关心当天，范围收窄到一天能显著减少拉取量。
    if (mode === 'day') return { from: anchor, to: anchor }
    if (mode === 'week') {
      const days = weekDays(anchor, weekStart)
      return { from: days[0], to: days[6] }
    }
    const weeks = monthMatrix(Number(anchor.slice(0, 4)), Number(anchor.slice(5, 7)) - 1, weekStart)
    return { from: weeks[0][0], to: weeks[5][6] }
  }, [mode, anchor, weekStart])

  useEffect(() => {
    let alive = true
    setLoading(true)
    api
      .listTasks({ from: range.from, to: range.to })
      .then((r) => {
        if (alive) setItems(r.tasks)
      })
      .catch(() => undefined)
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [range.from, range.to, version])

  const visible = useMemo(() => applyFilter(items, filter), [items, filter])

  const byDay = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const t of visible) {
      if (!t.dueDate) continue
      const arr = map.get(t.dueDate)
      if (arr) arr.push(t)
      else map.set(t.dueDate, [t])
    }
    for (const arr of map.values()) {
      arr.sort((a, b) => (a.dueTime ?? '99:99').localeCompare(b.dueTime ?? '99:99') || b.priority - a.priority)
    }
    return map
  }, [visible])

  const noDate = useMemo(() => visible.filter((t) => !t.dueDate && t.status !== 'done'), [visible])

  const onDrop = async (e: DragEvent<HTMLDivElement>, day: string) => {
    e.preventDefault()
    setDropDay(null)
    const raw = e.dataTransfer.getData(DRAG_MIME)
    const id = Number(raw)
    if (!id) return
    await moveTask(id, { dueDate: day })
  }

  const quickCreate = async (day: string) => {
    const title = draft.trim()
    setDraft('')
    setAdding(null)
    if (!title) return
    try {
      await api.createTask({ title, dueDate: day })
      toast(`已记入 ${day}`)
      const r = await api.listTasks({ from: range.from, to: range.to })
      setItems(r.tasks)
    } catch {
      toast('创建失败', 'error')
    }
  }

  /** 日视图：在某时刻落一件新任务。 */
  const createAt = async (time: string) => {
    const title = draft.trim()
    setDraft('')
    setAdding(null)
    if (!title) return
    try {
      await api.createTask({ title, dueDate: anchor, dueTime: time })
      toast(`已记入 ${anchor} ${time}`)
      const r = await api.listTasks({ from: range.from, to: range.to })
      setItems(r.tasks)
    } catch {
      toast('创建失败', 'error')
    }
  }

  /** 日视图：把任务拖到某个时刻，只改时间不动日期；传 null 表示退回「全天」。 */
  const moveToTime = async (id: number, time: string | null) => {
    await moveTask(id, { dueDate: anchor, dueTime: time })
  }

  const label =
    mode === 'month'
      ? `${anchor.slice(0, 4)} 年 ${Number(anchor.slice(5, 7))} 月`
      : mode === 'day'
        ? fullDate(anchor)
        : `${range.from} ~ ${range.to}`

  const shift = (dir: number) => {
    if (mode === 'month') setAnchor(addMonths(anchor, dir))
    else if (mode === 'day') setAnchor(addDays(anchor, dir))
    else setAnchor(addDays(anchor, dir * 7))
  }

  const today = todayStr()

  return (
    <div className="flex h-full flex-col">
      {/* 工具条 */}
      <div className="flex items-center gap-2 border-b border-line px-5 py-2.5">
        <div className="flex items-center gap-0.5">
          <IconButton icon={IconChevronLeft} label="上一段" onClick={() => shift(-1)} />
          <button
            type="button"
            onClick={() => setAnchor(todayStr())}
            className="rounded-lg border border-line px-2.5 py-1 text-[0.78125rem] text-ink-2 transition-colors hover:bg-surface-2"
          >
            回到今天
          </button>
          <IconButton icon={IconChevronRight} label="下一段" onClick={() => shift(1)} />
        </div>
        <h2 className="brand-serif ml-1 text-[0.9375rem] font-semibold text-ink">{label}</h2>
        {loading ? <span className="text-[0.71875rem] text-ink-3">载入中…</span> : null}
        <div className="ml-auto flex items-center gap-2">
          <span className="hidden text-[0.71875rem] text-ink-3 lg:block">
            {mode === 'day' ? '拖动任务到时间轴即可改时间，点空白处新建' : '拖动任务卡片可直接改期'}
          </span>
          <div className="flex rounded-lg border border-line p-0.5">
            {(['day', 'week', 'month'] as Mode[]).map((m) => (
              <button
                key={m}
                type="button"
                data-cal-mode={m}
                onClick={() => {
                  setMode(m)
                  // adding 在月/周视图存的是日期，在日视图存的是时刻，切模式必须清掉。
                  setAdding(null)
                  setDraft('')
                }}
                className={cx(
                  'rounded-md px-2.5 py-1 text-[0.78125rem] transition-colors',
                  mode === m ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
                )}
              >
                {m === 'month' ? '月' : m === 'week' ? '周' : '日'}
              </button>
            ))}
          </div>
          {mode !== 'day' ? (
            <button
              type="button"
              onClick={() => setWeekStart(weekStart === 1 ? 0 : 1)}
              className="rounded-lg border border-line px-2.5 py-1 text-[0.75rem] text-ink-2 transition-colors hover:bg-surface-2"
              title="切换每周起始日"
            >
              周始：{weekStart === 1 ? '周一' : '周日'}
            </button>
          ) : null}
        </div>
      </div>

      {/* 无日期抽屉 */}
      {noDate.length > 0 ? (
        <div className="flex items-center gap-2 overflow-x-auto border-b border-line bg-surface-2/50 px-5 py-2">
          <span className="shrink-0 text-[0.71875rem] text-ink-3">未排期 {noDate.length} 项，可直接拖到日历上：</span>
          {noDate.slice(0, 12).map((t) => (
            <span
              key={t.id}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData(DRAG_MIME, String(t.id))
                e.dataTransfer.effectAllowed = 'move'
              }}
              onClick={() => onOpen(t)}
              className="shrink-0 cursor-grab rounded-lg border border-line bg-surface px-2 py-0.5 text-[0.75rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal"
            >
              {t.title}
            </span>
          ))}
        </div>
      ) : null}

      {/* 主体 */}
      <div className="flex-1 overflow-auto px-5 py-3">
        {mode === 'month' ? (
          <MonthGrid
            anchor={anchor}
            weekStart={weekStart}
            byDay={byDay}
            today={today}
            dropDay={dropDay}
            setDropDay={setDropDay}
            onDrop={onDrop}
            onOpen={onOpen}
            adding={adding}
            setAdding={setAdding}
            draft={draft}
            setDraft={setDraft}
            onQuickCreate={quickCreate}
          />
        ) : mode === 'week' ? (
          <WeekGrid
            anchor={anchor}
            weekStart={weekStart}
            byDay={byDay}
            today={today}
            dropDay={dropDay}
            setDropDay={setDropDay}
            onDrop={onDrop}
            onOpen={onOpen}
            adding={adding}
            setAdding={setAdding}
            draft={draft}
            setDraft={setDraft}
            onQuickCreate={quickCreate}
          />
        ) : (
          <DayView
            day={anchor}
            today={today}
            tasks={byDay.get(anchor) ?? []}
            onOpen={onOpen}
            onMoveToTime={moveToTime}
            onCreateAt={createAt}
            adding={adding}
            setAdding={setAdding}
            draft={draft}
            setDraft={setDraft}
            onDropDay={onDrop}
          />
        )}
      </div>
    </div>
  )
}

interface GridProps {
  anchor: string
  weekStart: 0 | 1
  byDay: Map<string, Task[]>
  today: string
  dropDay: string | null
  setDropDay: (d: string | null) => void
  onDrop: (e: DragEvent<HTMLDivElement>, day: string) => void
  onOpen: (t: Task) => void
  adding: string | null
  setAdding: (d: string | null) => void
  draft: string
  setDraft: (v: string) => void
  onQuickCreate: (day: string) => void
}

function DayCell({
  day,
  tasks,
  today,
  isCurrentMonth,
  dropDay,
  setDropDay,
  onDrop,
  onOpen,
  adding,
  setAdding,
  draft,
  setDraft,
  onQuickCreate,
  compact,
}: {
  day: string
  tasks: Task[]
  today: string
  isCurrentMonth: boolean
  dropDay: string | null
  setDropDay: (d: string | null) => void
  onDrop: (e: DragEvent<HTMLDivElement>, day: string) => void
  onOpen: (t: Task) => void
  adding: string | null
  setAdding: (d: string | null) => void
  draft: string
  setDraft: (v: string) => void
  onQuickCreate: (day: string) => void
  compact?: boolean
}) {
  const isToday = day === today
  const overdue = dayDiff(day, today) < 0
  const visible = tasks.slice(0, compact ? 6 : 3)
  const rest = tasks.length - visible.length

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault()
        setDropDay(day)
      }}
      onDragLeave={() => setDropDay(null)}
      onDrop={(e) => void onDrop(e, day)}
      className={cx(
        'group/day relative flex min-h-[92px] flex-col gap-1 border-b border-r border-line p-1.5 transition-colors',
        isCurrentMonth ? 'bg-surface' : 'bg-surface-2/40',
        dropDay === day && 'drop-target',
      )}
    >
      <div className="flex items-center gap-1">
        <span
          className={cx(
            'grid h-5 min-w-5 place-items-center rounded-full px-1 text-[0.71875rem] tabular-nums',
            isToday ? 'bg-seal font-semibold text-seal-contrast' : isCurrentMonth ? 'text-ink-2' : 'text-ink-3',
            overdue && !isToday && 'text-p-high',
          )}
        >
          {Number(day.slice(8, 10))}
        </span>
          {overdue && tasks.some((t) => t.status !== 'done') ? (
          <span className="text-[0.59375rem] text-p-high">逾期</span>
        ) : null}
        <span
          role="button"
          tabIndex={-1}
          onClick={() => setAdding(adding === day ? null : day)}
          className="ml-auto opacity-0 transition-opacity group-hover/day:opacity-100"
          title="在这一天添加任务"
        >
          <IconPlus size={12} className="text-ink-3 hover:text-seal" />
        </span>
      </div>

      {visible.map((t) => (
        <button
          key={t.id}
          type="button"
          draggable
          onDragStart={(e) => {
            e.dataTransfer.setData(DRAG_MIME, String(t.id))
            e.dataTransfer.effectAllowed = 'move'
          }}
          onClick={() => onOpen(t)}
          className={cx(
            'flex w-full cursor-grab items-center gap-1 truncate rounded-md border px-1.5 py-0.5 text-left text-[0.71875rem] transition-colors',
            t.status === 'done'
              ? 'border-line bg-surface-2/60 text-ink-3 line-through'
              : t.priority === 3
                ? 'border-p-high/30 bg-p-high/8 text-ink hover:bg-p-high/14'
                : t.priority === 2
                  ? 'border-p-mid/30 bg-p-mid/8 text-ink hover:bg-p-mid/14'
                  : 'border-line bg-surface-2/70 text-ink hover:border-seal/35 hover:bg-seal/8',
          )}
          title={`${t.title}${t.dueTime ? ` · ${t.dueTime}` : ''}`}
        >
          {t.dueTime ? <span className="shrink-0 tabular-nums text-ink-3">{t.dueTime.slice(0, 5)}</span> : null}
          <PriorityFlag priority={t.priority} size={10} />
          <span className="truncate">{t.title}</span>
          {t.repeatRule ? <IconRepeat size={10} className="ml-auto shrink-0 text-ink-3" /> : null}
        </button>
      ))}

      {rest > 0 ? (
        <button
          type="button"
          onClick={() => onOpen(tasks[visible.length])}
          className="px-1 text-left text-[0.65625rem] text-ink-3 hover:text-seal"
        >
          还有 {rest} 项
        </button>
      ) : null}

      {adding === day ? (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            onQuickCreate(day)
          }}
          className="mt-auto flex items-center gap-1 rounded-md border border-seal/40 bg-surface px-1 py-0.5"
        >
          <input
            autoFocus
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={() => !draft.trim() && setAdding(null)}
            placeholder="标题，回车即存"
            className="min-w-0 flex-1 bg-transparent text-[0.71875rem] outline-none placeholder:text-ink-3"
          />
          <IconX size={11} className="shrink-0 cursor-pointer text-ink-3" onClick={() => setAdding(null)} />
        </form>
      ) : null}
    </div>
  )
}

function MonthGrid(props: GridProps) {
  const weeks = monthMatrix(Number(props.anchor.slice(0, 4)), Number(props.anchor.slice(5, 7)) - 1, props.weekStart)
  const monthIdx = Number(props.anchor.slice(5, 7))
  return (
    <div className="overflow-hidden rounded-xl border-l border-t border-line">
      <div className="grid grid-cols-7">
        {weekdayHeaders(props.weekStart).map((w) => (
          <div key={w} className="border-b border-r border-line bg-surface-2 px-2 py-1.5 text-[0.71875rem] text-ink-3">
            {w}
          </div>
        ))}
        {weeks.flat().map((day) => (
          <DayCell
            key={day}
            day={day}
            tasks={props.byDay.get(day) ?? []}
            today={props.today}
            isCurrentMonth={Number(day.slice(5, 7)) === monthIdx}
            dropDay={props.dropDay}
            setDropDay={props.setDropDay}
            onDrop={props.onDrop}
            onOpen={props.onOpen}
            adding={props.adding}
            setAdding={props.setAdding}
            draft={props.draft}
            setDraft={props.setDraft}
            onQuickCreate={props.onQuickCreate}
          />
        ))}
      </div>
    </div>
  )
}

function WeekGrid(props: GridProps) {
  const days = weekDays(props.anchor, props.weekStart)
  return (
    <div className="overflow-hidden rounded-xl border-l border-t border-line">
      <div className="grid grid-cols-7">
        {days.map((day) => (
          <div
            key={day}
            className={cx(
              'flex items-center justify-center gap-1.5 border-b border-r border-line bg-surface-2 px-2 py-1.5 text-[0.71875rem]',
              day === props.today ? 'text-seal' : 'text-ink-3',
            )}
          >
            {weekdayHeaders(props.weekStart)[days.indexOf(day)]}
            <span className={cx('tabular-nums', day === props.today && 'font-semibold')}>{Number(day.slice(8, 10))}</span>
          </div>
        ))}
        {days.map((day) => (
          <DayCell
            key={day}
            day={day}
            tasks={props.byDay.get(day) ?? []}
            today={props.today}
            isCurrentMonth
            compact
            dropDay={props.dropDay}
            setDropDay={props.setDropDay}
            onDrop={props.onDrop}
            onOpen={props.onOpen}
            adding={props.adding}
            setAdding={props.setAdding}
            draft={props.draft}
            setDraft={props.setDraft}
            onQuickCreate={props.onQuickCreate}
          />
        ))}
      </div>
      <div className="flex items-center gap-2 border-r border-b border-line bg-surface-2/40 px-3 py-2 text-[0.71875rem] text-ink-3">
        <IconClock size={12} />
        周视图按时间顺序列出当天事项；点击日期右上角的 + 可快速添加。
      </div>
    </div>
  )
}

interface DayProps {
  day: string
  today: string
  tasks: Task[]
  onOpen: (t: Task) => void
  onMoveToTime: (id: number, time: string | null) => void
  onCreateAt: (time: string) => void
  adding: string | null
  setAdding: (v: string | null) => void
  draft: string
  setDraft: (v: string) => void
  onDropDay: (e: DragEvent<HTMLDivElement>, day: string) => void
}

/** 日视图：24 小时时间轴，任务按时段成块排布，可拖动改时间、点空白处新建。 */
function DayView(props: DayProps) {
  const { day, today, tasks, onOpen, onMoveToTime, onCreateAt, adding, setAdding, draft, setDraft, onDropDay } = props
  const { compositionProps, isComposing } = useIMEGuard()
  const scroller = useRef<HTMLDivElement>(null)
  const [dragTime, setDragTime] = useState<string | null>(null)
  const [nowMin, setNowMin] = useState(() => toMinutes(nowHM()))

  useEffect(() => {
    const id = window.setInterval(() => setNowMin(toMinutes(nowHM())), 60_000)
    return () => window.clearInterval(id)
  }, [])

  // 分钟 → 时间轴 y 坐标；15 分钟一吸，避免出现 09:07 这种脏时刻。
  const yOf = (min: number) => min * PX_PER_MIN
  const snap = (y: number) => Math.min(DAY_MINUTES - SNAP_MIN, Math.max(0, Math.round(y / PX_PER_MIN / SNAP_MIN) * SNAP_MIN))

  // 进入某一天时定位到当前时刻（或 07:00），只定位一次，不随分钟刷新跳动。
  const nowRef = useRef(nowMin)
  nowRef.current = nowMin
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const focus = day === today ? nowRef.current : 7 * 60
    el.scrollTop = Math.max(0, yOf(focus) - el.clientHeight / 2)
  }, [day, today])

  const placed = useMemo(() => {
    const rows = tasks
      .filter((t) => t.dueTime)
      .map((t) => {
        const start = toMinutes(t.dueTime as string)
        // 块高优先用结束时间；没有则按预计时长，再退回默认 45 分。
        const dur = t.estimateMinutes > 0 ? t.estimateMinutes : DEFAULT_DURATION
        const raw = t.endTime ? toMinutes(t.endTime) : start + dur
        return { task: t, start, end: raw > start ? raw : start + dur }
      })
      .sort((a, b) => a.start - b.start || a.end - b.end)
    return placeDay(rows)
  }, [tasks])

  const allDay = useMemo(() => tasks.filter((t) => !t.dueTime), [tasks])

  const timeOf = (e: DragEvent<HTMLDivElement> | ReactMouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    return snap(e.clientY - rect.top)
  }

  const onSurfaceClick = (e: ReactMouseEvent<HTMLDivElement>) => {
    const min = timeOf(e)
    setDraft('')
    setAdding(minutesToHM(min))
  }

  const onSurfaceDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!e.dataTransfer.types.includes(DRAG_MIME)) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragTime(minutesToHM(timeOf(e)))
  }

  const onSurfaceDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    const min = timeOf(e)
    setDragTime(null)
    const id = Number(e.dataTransfer.getData(DRAG_MIME))
    if (id) void onMoveToTime(id, minutesToHM(min))
  }

  const empty = tasks.length === 0

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-xl border border-line">
      {/* 全天区：无时刻的任务；把时间轴上的块拖回这里可撤销时刻。 */}
      <div
        onDragOver={(e) => {
          if (!e.dataTransfer.types.includes(DRAG_MIME)) return
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
        }}
        onDrop={(e) => {
          e.preventDefault()
          setDragTime(null)
          const id = Number(e.dataTransfer.getData(DRAG_MIME))
          // 落到「全天」区即撤销时刻，任务仍留在这一天。
          if (id) void onMoveToTime(id, null)
        }}
        className="flex items-start gap-2 border-b border-line bg-surface-2/60 px-3 py-1.5"
        data-day-allday
      >
        <span className="mt-0.5 shrink-0 text-[0.6875rem] text-ink-3">全天</span>
        <div className="flex flex-1 flex-wrap items-center gap-1.5">
          {allDay.length === 0 ? (
            <span className="text-[0.71875rem] text-ink-3">无排定时刻的事项，拖到下方时间轴即可定下时刻</span>
          ) : (
            allDay.map((t) => (
              <button
                key={t.id}
                type="button"
                draggable
                onDragStart={(e) => {
                  e.dataTransfer.setData(DRAG_MIME, String(t.id))
                  e.dataTransfer.effectAllowed = 'move'
                }}
                onClick={() => onOpen(t)}
                className={cx(
                  'max-w-[220px] cursor-grab truncate rounded-md border px-2 py-0.5 text-[0.71875rem] transition-colors',
                  t.status === 'done'
                    ? 'border-line bg-surface text-ink-3 line-through'
                    : 'border-line bg-surface text-ink hover:border-seal/35 hover:text-seal',
                )}
                title={t.title}
              >
                {t.title}
              </button>
            ))
          )}
        </div>
      </div>

      {/* 时间轴 */}
      <div ref={scroller} className="relative flex-1 overflow-y-auto">
        {empty ? (
          <div className="pointer-events-none absolute inset-x-0 top-24 z-10 text-center text-[0.75rem] text-ink-3">
            这一天还是空白 —— 点时间轴的任意位置即可落下一件事
          </div>
        ) : null}

        <div className="relative" style={{ height: yOf(DAY_MINUTES) }}>
          {/* 左侧整点刻度 */}
          {HOURS.map((h) => (
            <span
              key={h}
              className="-mt-1.5 absolute w-[52px] pr-2.5 text-right text-[0.6875rem] tabular-nums text-ink-3"
              style={{ top: yOf(h * 60), left: 0 }}
            >
              {pad2(h)}:00
            </span>
          ))}

          {/* 排布区 */}
          <div
            onClick={onSurfaceClick}
            onDragOver={onSurfaceDragOver}
            onDragLeave={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragTime(null)
            }}
            onDrop={onSurfaceDrop}
            data-day-axis
            className="absolute inset-y-0 right-0 left-[52px] border-l border-line"
          >
            {HOURS.map((h) => (
              <div key={h} className="absolute right-0 left-0 h-px bg-line/70" style={{ top: yOf(h * 60) }} />
            ))}
            {/* 半点浅线，便于对齐 */}
            {HOURS.map((h) => (
              <div key={`half-${h}`} className="absolute right-0 left-0 h-px bg-line/35" style={{ top: yOf(h * 60 + 30) }} />
            ))}

            {/* 当前时刻 */}
            {day === today ? (
              <div className="absolute right-0 left-0 z-20 flex items-center" style={{ top: yOf(nowMin) }}>
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-seal" />
                <div className="h-px flex-1 bg-seal/70" />
                <span className="shrink-0 px-1 text-[0.625rem] tabular-nums text-seal">{minutesToHM(nowMin)}</span>
              </div>
            ) : null}

            {/* 拖拽落点指示 */}
            {dragTime ? (
              <div className="absolute right-0 left-0 z-30" style={{ top: yOf(toMinutes(dragTime)) }}>
                <div className="flex items-center gap-1">
                  <span className="rounded-sm bg-seal px-1 text-[0.625rem] tabular-nums text-seal-contrast">{dragTime}</span>
                  <div className="h-px flex-1 bg-seal" />
                </div>
              </div>
            ) : null}

            {/* 任务块 */}
            {placed.map((p) => (
              <button
                key={p.task.id}
                type="button"
                draggable
                data-day-block={p.task.id}
                onDragStart={(e) => {
                  e.stopPropagation()
                  e.dataTransfer.setData(DRAG_MIME, String(p.task.id))
                  e.dataTransfer.effectAllowed = 'move'
                }}
                onClick={(e) => {
                  e.stopPropagation()
                  onOpen(p.task)
                }}
                style={{
                  top: yOf(p.start) + 1,
                  height: Math.max(BLOCK_MIN_H, yOf(p.end - p.start)) - 2,
                  left: `calc(${(p.lane / p.lanes) * 100}% + 4px)`,
                  width: `calc(${100 / p.lanes}% - 8px)`,
                }}
                className={cx(
                  'absolute z-10 cursor-grab overflow-hidden rounded-lg border px-2 py-1 text-left transition-colors',
                  p.task.status === 'done'
                    ? 'border-line bg-surface-2/70 text-ink-3 line-through'
                    : p.task.priority === 3
                      ? 'border-p-high/40 bg-p-high/10 text-ink hover:bg-p-high/16'
                      : p.task.priority === 2
                        ? 'border-p-mid/40 bg-p-mid/10 text-ink hover:bg-p-mid/16'
                        : 'border-seal/30 bg-seal/8 text-ink hover:bg-seal/14',
                )}
                title={`${p.task.title} · ${minutesToHM(p.start)}–${minutesToHM(p.end)}`}
              >
                <span className="flex items-center gap-1">
                  <PriorityFlag priority={p.task.priority} size={10} />
                  <span className="truncate text-[0.75rem] font-medium">{p.task.title}</span>
                  {p.task.repeatRule ? <IconRepeat size={10} className="ml-auto shrink-0 text-ink-3" /> : null}
                </span>
                <span className="block truncate text-[0.65625rem] tabular-nums text-ink-3">
                  {minutesToHM(p.start)} – {minutesToHM(p.end)}
                  {p.task.endTime
                    ? ''
                    : p.task.estimateMinutes > 0
                      ? `（预计 ${p.task.estimateMinutes} 分）`
                      : '（默认 45 分）'}
                </span>
              </button>
            ))}

            {/* 就地点建 */}
            {isTimeStr(adding) ? (
              <form
                onClick={(e) => e.stopPropagation()}
                onSubmit={(e) => {
                  e.preventDefault()
                  onCreateAt(adding)
                }}
                style={{ top: yOf(toMinutes(adding)) + 1 }}
                data-day-add={adding}
                className="absolute right-2 left-1 z-40 flex items-center gap-1 rounded-lg border border-seal/50 bg-surface px-2 py-1 shadow-sm"
              >
                <span className="shrink-0 text-[0.6875rem] tabular-nums text-seal">{adding}</span>
                <input
                  autoFocus
                  {...compositionProps}
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => !isComposing(e) && e.key === 'Escape' && setAdding(null)}
                  onBlur={() => !draft.trim() && setAdding(null)}
                  placeholder="标题，回车即存"
                  className="min-w-0 flex-1 bg-transparent text-[0.75rem] outline-none placeholder:text-ink-3"
                />
                <IconX size={12} className="shrink-0 cursor-pointer text-ink-3" onClick={() => setAdding(null)} />
              </form>
            ) : null}
          </div>
        </div>
      </div>

      {/* 底部提示：拖回全天区可撤销时刻 */}
      <div className="flex items-center gap-2 border-t border-line bg-surface-2/40 px-3 py-1.5 text-[0.71875rem] text-ink-3">
        <IconClock size={12} />
        共 {placed.length} 项排定时刻、{allDay.length} 项全天；把块拖到顶部「全天」区可去掉时刻。
      </div>
    </div>
  )
}

