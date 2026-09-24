import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'

import { api } from '../api/client'
import { addDays, todayStr, weekday, weekdayName } from '../lib/date'
import { colorName } from '../lib/palette'
import { useStore } from '../store/AppStore'
import type { Habit, HabitBoard, HabitCadence, HabitLog, HabitStat } from '../types'
import { IconFlame, IconMore, IconPlus, IconSeedling, IconTrash } from './icons'
import { Button, ColorDot, EmptyState, Field, MenuItem, Modal, Popover, RoundCheck, cx, inputClass } from './ui'

/** 可选的观察区间长度（周）。半年的跨度最能看出「积日成习」。 */
const RANGES: { weeks: number; label: string }[] = [
  { weeks: 12, label: '近 12 周' },
  { weeks: 26, label: '近 26 周' },
]
const DEFAULT_WEEKS = 26

const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0] // 周一 → 周日，表单与标签统一用这个顺序
const WEEK_SHORT = ['日', '一', '二', '三', '四', '五', '六']
const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6]
const PALETTE = ['#6b7f6e', '#b4553d', '#8a6d3b', '#5c6b8a', '#7a5c7a', '#3f7a7a', '#8a7c66']
const NAME_COL = 92 // 热力图左侧习惯名列宽（px）
const CELL = 13 // 热力图格子边长（px）
const CELL_GAP = 4 // 格子间距（px）

const WEEKDAY_PRESETS: { label: string; days: number[] }[] = [
  { label: '每天', days: ALL_DAYS },
  { label: '工作日', days: [1, 2, 3, 4, 5] },
  { label: '周末', days: [0, 6] },
]

/** 弹出层用的表单草稿。 */
interface HabitDraft {
  id?: number
  name: string
  icon: string
  color: string
  cadence: HabitCadence
  weekdays: number[]
  target: number
  startDate: string
  note: string
}

function emptyDraft(): HabitDraft {
  return {
    name: '',
    icon: 'habit',
    color: PALETTE[0],
    cadence: 'daily',
    weekdays: ALL_DAYS,
    target: 1,
    startDate: todayStr(),
    note: '',
  }
}

function draftOf(h: Habit): HabitDraft {
  const days = h.weekdays
    ? h.weekdays.split(',').map(Number).filter((n) => n >= 0 && n <= 6)
    : []
  return {
    id: h.id,
    name: h.name,
    icon: h.icon,
    color: h.color,
    cadence: h.cadence,
    weekdays: days.length ? days : ALL_DAYS,
    target: h.target,
    startDate: h.startDate,
    note: h.note,
  }
}

/** 某天是否属于该习惯的排期日（与后端 habitScheduled 口径一致）。 */
function scheduled(h: Habit, day: string): boolean {
  if (day < h.startDate) return false
  if (h.cadence !== 'weekly') return true
  const days = h.weekdays ? h.weekdays.split(',').map(Number) : []
  return days.length === 0 || days.includes(weekday(day))
}

function weeklyLabel(weekdays: string): string {
  const days = (weekdays ? weekdays.split(',').map(Number) : [])
    .filter((n) => n >= 0 && n <= 6)
    .sort((a, b) => WEEK_ORDER.indexOf(a) - WEEK_ORDER.indexOf(b))
  if (days.length === 0 || days.length === 7) return '每天'
  if (days.length === 5 && days.every((d) => d >= 1 && d <= 5)) return '工作日'
  if (days.length === 2 && days.includes(0) && days.includes(6)) return '周末'
  return '每周' + days.map((d) => WEEK_SHORT[d]).join('')
}

/**
 * 习惯打卡：以「日」为单位的长期坚持。
 * 上半是今日待办，下半是 12/26 周的热力图——点格子即可补记或撤销某一天。
 */
export function HabitsView() {
  const { settings, toast, confirm } = useStore()
  const [weeks, setWeeks] = useState(DEFAULT_WEEKS)
  const [board, setBoard] = useState<HabitBoard | null>(null)
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  const [draft, setDraft] = useState<HabitDraft | null>(null)
  const [menuFor, setMenuFor] = useState<number | null>(null)
  const [busy, setBusy] = useState<number | null>(null)
  const [showArchived, setShowArchived] = useState(false)

  const weekStart = (settings.weekStart === '0' ? 0 : 1) as 0 | 1

  // 热力图按周对齐：最后一列是本周，往前推 weeks 列。
  const grid = useMemo(() => {
    const t = todayStr()
    const end = addDays(t, 6 - ((weekday(t) - weekStart + 7) % 7))
    const start = addDays(end, -(weeks * 7 - 1))
    const cells: string[] = []
    for (let i = 0; i < weeks * 7; i++) cells.push(addDays(start, i))
    // 每列的最后一天，用来判断该列的月份（月份只在换月那一列标一次）
    const labels = Array.from({ length: weeks }, (_, w) => cells[w * 7 + 6])
    return { start, end, cells, labels }
  }, [weeks, weekStart])

  useEffect(() => {
    let alive = true
    setLoading(true)
    api
      .listHabits({ from: grid.start, to: grid.end, includeArchived: showArchived ? '1' : undefined })
      .then((b) => {
        if (alive) setBoard(b)
      })
      .catch(() => undefined)
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [grid.start, grid.end, reload, showArchived])

  const today = board?.today ?? todayStr()
  const habits = board?.habits ?? []
  const stats = useMemo(() => {
    const m = new Map<number, HabitStat>()
    for (const s of board?.stats ?? []) m.set(s.habitId, s)
    return m
  }, [board])

  /** habitId|day → 打卡记录，供热力图渲染。 */
  const logIndex = useMemo(() => {
    const m = new Map<string, HabitLog>()
    for (const l of board?.logs ?? []) m.set(`${l.habitId}|${l.day}`, l)
    return m
  }, [board])

  const summary = useMemo(() => {
    // 汇总只算未归档：归档是「收起来」，不该再占用今日分母与达标率。
    const active = new Set((board?.habits ?? []).filter((h) => !h.archived).map((h) => h.id))
    const list = (board?.stats ?? []).filter((s) => active.has(s.habitId))
    const done = list.reduce((n, s) => n + s.done, 0)
    const due = list.reduce((n, s) => n + s.due, 0)
    return { done, due, rate: due ? done / due : 0, hit: list.filter((s) => s.today).length, active: active.size }
  }, [board])

  const refresh = useCallback(() => setReload((n) => n + 1), [])

  // 连点守卫：同一习惯的请求在途时忽略后续点击。
  // 否则"记一次"与"撤一次"两次请求会以不可预期的顺序落库，
  // 结果既不等于一次也不等于两次（对比 MorningPlan 的 acting Set 写法）。
  const inFlight = useRef<Set<number>>(new Set())

  const run = useCallback(
    async (habit: Habit, fn: () => Promise<unknown>, failMsg: string) => {
      if (inFlight.current.has(habit.id)) return
      inFlight.current.add(habit.id)
      setBusy(habit.id)
      try {
        await fn()
        refresh()
      } catch {
        toast(failMsg, 'error')
      } finally {
        inFlight.current.delete(habit.id)
        setBusy(null)
      }
    },
    [refresh, toast],
  )

  /** 一键达标 / 撤销今日。目标多于一次时一次记满，避免连点。 */
  const toggleToday = (habit: Habit, stat: HabitStat | undefined) => {
    if (habit.archived) return
    if (stat?.today) {
      void run(habit, () => api.uncheckHabit(habit.id, today), '撤销失败')
      return
    }
    void run(habit, () => api.checkHabit(habit.id, { day: today, count: habit.target }), '打卡失败')
  }

  /** 分次记：目标为 N 次时一次一次累加。 */
  const bumpToday = (habit: Habit) => {
    if (habit.archived) return
    void run(habit, () => api.checkHabit(habit.id, { day: today }), '打卡失败')
  }

  /** 点热力格的某一天即补记或撤销那一天。 */
  const toggleDay = (habit: Habit, day: string) => {
    if (habit.archived || day > today) return
    const met = (logIndex.get(`${habit.id}|${day}`)?.count ?? 0) >= habit.target
    void run(
      habit,
      () => (met ? api.uncheckHabit(habit.id, day) : api.checkHabit(habit.id, { day, count: habit.target })),
      '更新失败',
    )
  }

  /**
   * 热力图的键盘导航。
   *
   * 配合 HeatCell 的 roving tabindex（每个习惯只有「今天」那一格可 Tab），
   * 在网格内用方向键移动：左右跨周、上下换星期。
   * 不这么做的话，26 周 × 7 天 = 182 个格子会变成 182 个 Tab 停靠点。
   */
  const moveInGrid = (e: ReactKeyboardEvent<HTMLDivElement>, habitId: number) => {
    const attr = (e.target as HTMLElement).getAttribute('data-habit-cell')
    if (!attr) return
    const index = grid.cells.indexOf(attr.split('|')[1])
    if (index < 0) return
    let next = index
    if (e.key === 'ArrowLeft') next = index - 7
    else if (e.key === 'ArrowRight') next = index + 7
    else if (e.key === 'ArrowUp') next = index - 1
    else if (e.key === 'ArrowDown') next = index + 1
    else return
    if (next < 0 || next >= grid.cells.length) return
    e.preventDefault()
    const target = e.currentTarget.querySelector<HTMLElement>(`[data-habit-cell="${habitId}|${grid.cells[next]}"]`)
    target?.focus()
    target?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }

  const saveDraft = async () => {
    if (!draft) return
    const name = draft.name.trim()
    if (!name) {
      toast('习惯名称不能为空', 'error')
      return
    }
    const weekdays = [...new Set(draft.weekdays)].sort((a, b) => a - b).join(',')
    if (draft.cadence === 'weekly' && weekdays === '') {
      toast('按周重复至少选一天', 'error')
      return
    }
    const body = {
      name,
      icon: draft.icon,
      color: draft.color,
      cadence: draft.cadence,
      // 每天重复时清空星期，避免残留旧值造成误会
      weekdays: draft.cadence === 'weekly' ? weekdays : '',
      target: Math.max(1, Math.min(99, Number(draft.target) || 1)),
      startDate: draft.startDate,
      note: draft.note,
    }
    try {
      if (draft.id) await api.updateHabit(draft.id, body)
      else await api.createHabit(body)
      toast(draft.id ? '已保存' : `已立「${name}」`)
      setDraft(null)
      refresh()
    } catch {
      toast('保存失败', 'error')
    }
  }

  const remove = async (habit: Habit) => {
    setMenuFor(null)
    // 全站唯一还在用原生 confirm() 的地方：样式不可控、文案口径不一致，
    // 在 iframe / 部分浏览器里还会被直接拦掉。统一走应用内的确认框。
    const ok = await confirm({
      title: `删除习惯「${habit.name}」`,
      message: '打卡记录会一并删除，且不可恢复。',
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return
    try {
      await api.deleteHabit(habit.id)
      toast('已删除')
      refresh()
    } catch {
      toast('删除失败', 'error')
    }
  }

  const archive = async (habit: Habit) => {
    setMenuFor(null)
    try {
      await api.updateHabit(habit.id, { archived: true })
      toast(`已归档「${habit.name}」，勾选「显示已归档」可找回`)
      refresh()
    } catch {
      toast('归档失败', 'error')
    }
  }

  const restore = async (habit: Habit) => {
    setMenuFor(null)
    try {
      await api.updateHabit(habit.id, { archived: false })
      toast(`已恢复「${habit.name}」`)
      refresh()
    } catch {
      toast('恢复失败', 'error')
    }
  }

  // 真空 = 服务端这次没给任何行（开关开着时归档行也在内，有行就该渲染列表）。
  const showEmpty = habits.length === 0 && !loading

  return (
    <div className="flex h-full flex-col">
      {/* 工具条：标题由上层工具栏给出，这里只放区间与统计。 */}
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2.5 md:px-5">
        {loading ? <span className="text-[0.71875rem] text-ink-3">载入中…</span> : null}
        <span className="text-[0.75rem] text-ink-3">
          今日已打卡 <span className="tabular-nums text-ink-2">{summary.hit}</span> / {summary.active}
          {summary.due > 0 ? (
            <>
              {' · '}
              {weeks} 周达标率 <span className="tabular-nums text-ink-2">{Math.round(summary.rate * 100)}%</span>
            </>
          ) : null}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <div className="flex rounded-lg border border-line p-0.5">
            {RANGES.map((r) => (
              <button
                key={r.weeks}
                type="button"
                onClick={() => setWeeks(r.weeks)}
                className={cx(
                  'rounded-md px-2.5 py-1 text-[0.78125rem] transition-colors',
                  weeks === r.weeks ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
                )}
              >
                {r.label}
              </button>
            ))}
          </div>
          <button
            type="button"
            data-testid="toggle-archived-habits"
            onClick={() => setShowArchived((v) => !v)}
            className={cx(
              'rounded-lg border px-2.5 py-1.5 text-[0.78125rem] transition-colors',
              showArchived ? 'border-seal/45 bg-seal/12 font-medium text-seal' : 'border-line text-ink-2 hover:text-ink',
            )}
          >
            {showArchived ? '隐藏已归档' : '显示已归档'}
          </button>
          <Button variant="primary" onClick={() => setDraft(emptyDraft())}>
            <IconPlus size={14} />
            新建习惯
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-auto px-5 py-4">
        {showEmpty ? (
          <EmptyState
            text="习惯是日日不断的功夫，不必多，一件足矣。"
            source="《礼记·中庸》：致中和，天地位焉，万物育焉。"
            hint="立一件每天要做的小事，之后在这里打卡即可。"
            action={
              <Button variant="primary" onClick={() => setDraft(emptyDraft())}>
                立第一个习惯
              </Button>
            }
          />
        ) : (
          <div className="mx-auto flex max-w-[900px] flex-col gap-6">
            {/* 今日 */}
            <section>
              <div className="mb-2 flex items-center gap-2">
                <span className="text-[0.65625rem] font-semibold tracking-[0.14em] text-ink-3">今日</span>
                <span className="text-[0.6875rem] text-ink-3">{weekdayName(today)}</span>
                <span className="ml-auto text-[0.6875rem] text-ink-3">右侧「+1」用于分次记录</span>
              </div>
              <div className="flex flex-col gap-1.5">
                {habits.map((h) => (
                  <HabitRow
                    key={h.id}
                    habit={h}
                    stat={stats.get(h.id)}
                    busy={busy === h.id}
                    menuOpen={menuFor === h.id}
                    onMenu={(open) => setMenuFor(open ? h.id : null)}
                    onToggle={() => toggleToday(h, stats.get(h.id))}
                    onBump={() => bumpToday(h)}
                    onEdit={() => {
                      setMenuFor(null)
                      setDraft(draftOf(h))
                    }}
                    onArchive={() => void archive(h)}
                    onRestore={() => void restore(h)}
                    onDelete={() => void remove(h)}
                  />
                ))}
              </div>
            </section>

            {/* 热力图 */}
            <section>
              <div className="mb-2 flex items-center gap-2">
                <span className="text-[0.65625rem] font-semibold tracking-[0.14em] text-ink-3">坚持轨迹</span>
                <span className="text-[0.6875rem] tabular-nums text-ink-3">
                  {grid.start} 至 {grid.end}
                </span>
                <span className="ml-auto text-[0.6875rem] text-ink-3">点格子可补记或撤销那一天</span>
              </div>
              <div className="overflow-x-auto pb-1">
                <div className="inline-flex flex-col rounded-xl border border-line bg-surface p-3">
                  {/* 月份刻度：与下方格子逐列对齐 */}
                  <div className="mb-1 flex" style={{ paddingLeft: NAME_COL + CELL_GAP, gap: CELL_GAP }}>
                    {grid.labels.map((d, i) => {
                      const prev = i > 0 ? grid.labels[i - 1].slice(0, 7) : ''
                      return (
                        <span
                          key={d}
                          className="shrink-0 text-[0.59375rem] whitespace-nowrap text-ink-3"
                          style={{ width: CELL }}
                        >
                          {prev !== d.slice(0, 7) ? `${Number(d.slice(5, 7))}月` : ''}
                        </span>
                      )
                    })}
                  </div>
                  {habits.map((h) => (
                    <div key={h.id} className={cx('flex items-center', h.archived && 'opacity-45')} style={{ gap: CELL_GAP }}>
                      <span
                        className="shrink-0 truncate pr-2 text-right text-[0.71875rem] text-ink-2"
                        style={{ width: NAME_COL }}
                        title={h.archived ? `${h.name}（已归档）` : h.name}
                      >
                        {h.name}
                      </span>
                      {/* 周为列、星期为行：cells 按「周优先」排列，配合 column 流向即可对齐 */}
                      <div
                        className="grid"
                        onKeyDown={(e) => moveInGrid(e, h.id)}
                        style={{
                          gridTemplateRows: `repeat(7, ${CELL}px)`,
                          gridAutoFlow: 'column',
                          gap: CELL_GAP,
                        }}
                      >
                        {grid.cells.map((day) => (
                          <HeatCell
                            key={day}
                            habit={h}
                            day={day}
                            today={today}
                            count={logIndex.get(`${h.id}|${day}`)?.count ?? 0}
                            tabbable={day === today}
                            onClick={() => toggleDay(h, day)}
                          />
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <div className="mt-2 flex items-center gap-3 text-[0.65625rem] text-ink-3">
                <span className="flex items-center gap-1">
                  <span className="h-[11px] w-[11px] rounded-[3px] bg-surface-2 ring-1 ring-line ring-inset" />
                  未达标
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-[11px] w-[11px] rounded-[3px]" style={{ background: 'color-mix(in oklab, var(--seal) 45%, transparent)' }} />
                  部分达成
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-[11px] w-[11px] rounded-[3px]" style={{ background: 'var(--seal)' }} />
                  已达标
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-[11px] w-[11px] rounded-[3px] bg-surface-2/40" />
                  非排期 / 未到
                </span>
              </div>
            </section>
          </div>
        )}
      </div>

      {/* 新建 / 编辑 */}
      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        title={draft?.id ? '编辑习惯' : '立一个习惯'}
        subtitle="慎始而敬终：先定下节奏与目标，再谈坚持。"
        footer={
          <>
            <Button onClick={() => setDraft(null)}>取消</Button>
            <Button variant="primary" onClick={() => void saveDraft()}>
              {draft?.id ? '保存' : '立下'}
            </Button>
          </>
        }
      >
        {draft ? <HabitForm draft={draft} onChange={setDraft} /> : null}
      </Modal>
    </div>
  )
}

/* ---------------- 今日行 ---------------- */

function HabitRow({
  habit,
  stat,
  busy,
  menuOpen,
  onMenu,
  onToggle,
  onBump,
  onEdit,
  onArchive,
  onRestore,
  onDelete,
}: {
  habit: Habit
  stat?: HabitStat
  busy: boolean
  menuOpen: boolean
  onMenu: (open: boolean) => void
  onToggle: () => void
  onBump: () => void
  onEdit: () => void
  onArchive: () => void
  onRestore: () => void
  onDelete: () => void
}) {
  const done = stat?.today ?? false
  const at = stat?.todayAt ?? 0
  const archived = habit.archived
  const cadence = habit.cadence === 'weekly' ? weeklyLabel(habit.weekdays) : '每天'

  return (
    <div
      data-habit-row={habit.id}
      data-habit-done={done ? '1' : '0'}
      data-habit-archived={archived ? '1' : '0'}
      className={cx(
        'group/habit relative flex items-center gap-3 rounded-xl border px-3 py-2 transition-colors',
        archived
          ? 'border-line bg-surface-2/50 opacity-60'
          : done
            ? 'border-seal/25 bg-seal/6'
            : 'border-line bg-surface hover:border-line-strong',
      )}
    >
      {archived ? (
        <span className="grid h-5 w-5 shrink-0 place-items-center text-[0.65625rem] text-ink-3" title="已归档，暂停打卡">
          档
        </span>
      ) : (
        <RoundCheck
          checked={done}
          color={habit.color}
          size={20}
          disabled={busy}
          title={done ? '撤销今日打卡' : '今日打卡'}
          onChange={onToggle}
        />
      )}

      <span className="h-7 w-[3px] shrink-0 rounded-full" style={{ background: habit.color }} />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={cx('truncate text-[0.84375rem] font-medium', done && !archived ? 'text-ink-2' : 'text-ink')}>
            {habit.name}
          </span>
          {archived ? (
            <span className="shrink-0 rounded border border-line px-1 text-[0.65625rem] text-ink-3">已归档</span>
          ) : habit.target > 1 ? (
            <span
              data-habit-progress={habit.id}
              className="shrink-0 rounded border border-line px-1 text-[0.65625rem] tabular-nums text-ink-3"
            >
              {at}/{habit.target}
            </span>
          ) : null}
        </div>
        <div className="mt-0.5 flex items-center gap-2 text-[0.6875rem] text-ink-3">
          <span>{cadence}</span>
          {habit.note ? <span className="truncate">· {habit.note}</span> : null}
        </div>
      </div>

      {!archived && habit.target > 1 && !done ? (
        <button
          type="button"
          onClick={onBump}
          disabled={busy}
          title="再记一次"
          className="shrink-0 rounded-md border border-line px-1.5 py-0.5 text-[0.6875rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal disabled:opacity-50"
        >
          +1
        </button>
      ) : null}

      {!archived && stat && stat.streak > 0 ? (
        <span
          className="flex shrink-0 items-center gap-1 rounded-md border border-p-high/25 bg-p-high/8 px-1.5 py-0.5 text-[0.6875rem] tabular-nums text-p-high"
          title={`当前连续 ${stat.streak} · 历史最长 ${stat.best}`}
        >
          <IconFlame size={12} />
          {stat.streak} 天
        </span>
      ) : !archived ? (
        <span className="shrink-0 text-[0.6875rem] text-ink-3" title={stat ? `历史最长 ${stat.best}` : ''}>
          尚未连成
        </span>
      ) : null}

      <div className="shrink-0">
        <button
          type="button"
          title="更多"
          onClick={() => onMenu(!menuOpen)}
          className="rounded-md p-1 text-ink-3 opacity-0 transition-opacity group-hover/habit:opacity-100 hover:bg-surface-2 hover:text-ink"
        >
          <IconMore size={15} />
        </button>
        <Popover open={menuOpen} onClose={() => onMenu(false)} align="right" width={200}>
          <MenuItem onClick={onEdit}>编辑</MenuItem>
          {archived ? (
            <MenuItem onClick={onRestore}>恢复</MenuItem>
          ) : (
            <MenuItem onClick={onArchive}>归档</MenuItem>
          )}
          <MenuItem onClick={onDelete} icon={IconTrash} danger>
            删除
          </MenuItem>
        </Popover>
      </div>
    </div>
  )
}

/* ---------------- 热力图格子 ---------------- */

function HeatCell({
  habit,
  day,
  today,
  count,
  tabbable,
  onClick,
}: {
  habit: Habit
  day: string
  today: string
  count: number
  /** 每个习惯只保留一个 Tab 入口（今天那一格），其余靠方向键在网格内移动。
      26 周 × 7 天 = 182 个格子全可 Tab 的话，键盘用户根本走不出去。 */
  tabbable: boolean
  onClick: () => void
}) {
  const future = day > today
  const met = count >= habit.target
  const partial = count > 0 && !met
  const owed = !met && !partial && !future && scheduled(habit, day)
  // 状态写进可访问名：颜色之外必须还有一条非视觉通道（WCAG 1.4.1）
  const stateWord = met
    ? '已达标'
    : partial
      ? `部分达成 ${count}/${habit.target}`
      : owed
        ? '未达标'
        : future
          ? '未到'
          : '非排期'
  const title = `${habit.name} · ${day} · ${stateWord}`

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={future}
      tabIndex={tabbable && !future ? 0 : -1}
      title={title}
      aria-label={title}
      data-habit-cell={`${habit.id}|${day}`}
      data-habit-cell-state={met ? 'done' : partial ? 'partial' : owed ? 'owed' : future ? 'future' : 'idle'}
      className={cx(
        'h-[13px] w-[13px] shrink-0 rounded-[3px] transition-transform',
        future ? 'cursor-default bg-surface-2/40' : 'hover:scale-[1.18]',
        // 「该做没做」用带描边的实心块，「非排期」用无描边的浅块——
        // 两者此前只有明度差别，浅色主题下几乎分不出来。
        owed && 'bg-surface-3 ring-1 ring-ink-3/45 ring-inset',
        partial && 'bg-seal/45',
        met && 'bg-seal',
        !owed && !partial && !met && !future && 'bg-surface-2/40',
      )}
      style={count > 0 ? { background: count >= habit.target ? habit.color : `color-mix(in oklab, ${habit.color} 45%, transparent)` } : undefined}
    />
  )
}

/* ---------------- 表单 ---------------- */

function HabitForm({ draft, onChange }: { draft: HabitDraft; onChange: (d: HabitDraft) => void }) {
  const set = <K extends keyof HabitDraft>(key: K, value: HabitDraft[K]) => onChange({ ...draft, [key]: value })
  const toggleDay = (wd: number) => {
    const has = draft.weekdays.includes(wd)
    set('weekdays', has ? draft.weekdays.filter((d) => d !== wd) : [...draft.weekdays, wd])
  }

  return (
    <div className="flex flex-col gap-4">
      <Field label="名称">
        <input
          autoFocus
          value={draft.name}
          onChange={(e) => set('name', e.target.value)}
          placeholder="例如：晨起临帖、日饮八杯水"
          className={inputClass}
        />
      </Field>

      <Field label="节奏">
        <div className="flex flex-wrap items-center gap-2">
          <div role="group" aria-label="节奏" className="flex rounded-lg border border-line p-0.5">
            {(['daily', 'weekly'] as HabitCadence[]).map((c) => (
              <button
                key={c}
                type="button"
                aria-pressed={draft.cadence === c}
                onClick={() => set('cadence', c)}
                className={cx(
                  'rounded-md px-2.5 py-1 text-[0.78125rem] transition-colors',
                  draft.cadence === c ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
                )}
              >
                {c === 'daily' ? '每天' : '按周'}
              </button>
            ))}
          </div>
          {draft.cadence === 'weekly'
            ? WEEKDAY_PRESETS.map((p) => (
                <button
                  key={p.label}
                  type="button"
                  aria-pressed={
                    p.days.length === draft.weekdays.length && p.days.every((d) => draft.weekdays.includes(d))
                  }
                  onClick={() => set('weekdays', p.days)}
                  className="rounded-md border border-line px-2 py-0.5 text-[0.71875rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal"
                >
                  {p.label}
                </button>
              ))
            : null}
        </div>
        {draft.cadence === 'weekly' ? (
          <div className="mt-2 flex items-center gap-1">
            {WEEK_ORDER.map((wd) => {
              const on = draft.weekdays.includes(wd)
              return (
                <button
                  key={wd}
                  type="button"
                  aria-label={`周${WEEK_SHORT[wd]}`}
                  aria-pressed={on}
                  onClick={() => toggleDay(wd)}
                  className={cx(
                    'grid h-8 w-8 place-items-center rounded-lg border text-[0.75rem] transition-colors',
                    on ? 'border-seal/45 bg-seal/12 font-medium text-seal' : 'border-line text-ink-2 hover:text-ink',
                  )}
                >
                  {WEEK_SHORT[wd]}
                </button>
              )
            })}
          </div>
        ) : null}
      </Field>

      <div className="grid grid-cols-2 gap-4">
        <Field label="达标次数" hint="每次达标所需次数，日常习惯填 1 即可。">
          <input
            type="number"
            min={1}
            max={99}
            value={draft.target}
            onChange={(e) => set('target', Number(e.target.value))}
            className={inputClass}
          />
        </Field>
        <Field label="起始日" hint="此前的日子不计入欠账与达标率。">
          <input
            type="date"
            value={draft.startDate}
            onChange={(e) => set('startDate', e.target.value)}
            className={inputClass}
          />
        </Field>
      </div>

      <Field label="备注">
        <input
          value={draft.note}
          onChange={(e) => set('note', e.target.value)}
          placeholder="为何立这一条？（选填）"
          className={inputClass}
        />
      </Field>

      <Field label="标记色">
        <div className="flex items-center gap-2">
          {PALETTE.map((c) => (
            <button
              key={c}
              type="button"
              aria-label={colorName(c)}
              title={colorName(c)}
              onClick={() => set('color', c)}
              className={cx(
                'grid h-7 w-7 place-items-center rounded-full border-2 transition-colors',
                draft.color === c ? 'border-seal' : 'border-transparent hover:border-line-strong',
              )}
            >
              <ColorDot color={c} size={16} />
            </button>
          ))}
        </div>
      </Field>

      <p className="flex items-center gap-1.5 rounded-lg border border-line bg-surface-2/60 px-3 py-2 text-[0.71875rem] text-ink-3">
        <IconSeedling size={13} className="shrink-0" />
        习惯与任务分开记：任务做完就结束，习惯只在时间里长出来。
      </p>
    </div>
  )
}
