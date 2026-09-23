import { useEffect, useState } from 'react'

import { api } from '../api/client'
import { addDays, fullDate, todayStr } from '../lib/date'
import { QUOTES, morningLine, reviewLine } from '../lib/quotes'
import { useNotifyDiagnosis } from '../lib/notify'
import { useStore } from '../store/AppStore'
import type { Task } from '../types'
import {
  IconAlert,
  IconBell,
  IconBook,
  IconCheck,
  IconClock,
  IconInfo,
  IconSunrise,
  IconTimer,
  IconX,
} from './icons'
import { Button, Modal, ProgressRing, RoundCheck, cx, inputClass } from './ui'

/* ============================================================
   全局覆盖层集合：晨省、日省、专注、确认框、提醒中心、轻提示。
   放在一处便于在 App 里一次挂载。
   ============================================================ */

export function AppOverlays() {
  const [morningOpen, setMorningOpen] = useState(false)
  const [reviewOpen, setReviewOpen] = useState(false)
  const [focusOpen, setFocusOpen] = useState(false)

  useEffect(() => {
    const onMorning = () => setMorningOpen(true)
    const onReview = () => setReviewOpen(true)
    const onFocus = () => setFocusOpen(true)
    window.addEventListener('shenshi:morning', onMorning)
    window.addEventListener('shenshi:review', onReview)
    window.addEventListener('shenshi:focus', onFocus)
    return () => {
      window.removeEventListener('shenshi:morning', onMorning)
      window.removeEventListener('shenshi:review', onReview)
      window.removeEventListener('shenshi:focus', onFocus)
    }
  }, [])

  return (
    <>
      <MorningPlan open={morningOpen} onClose={() => setMorningOpen(false)} />
      <DailyReview open={reviewOpen} onClose={() => setReviewOpen(false)} />
      <FocusPanel open={focusOpen} onClose={() => setFocusOpen(false)} />
      <ReminderCenter />
      <ConfirmHost />
      <ToastHost />
      <AutoRituals onMorning={() => setMorningOpen(true)} onReview={() => setReviewOpen(true)} />
    </>
  )
}

/* ---------------- 自动触发 ---------------- */

/** 每日首次打开时引导晨省；晚间首次打开时引导日省。各自每天只出现一次。
 *  判定以服务端 settings 为准，另用 localStorage 记「今天已弹过」作兜底：
 *  即便保存接口失败（后端未起、断网等），也不会在当天反复打扰。 */
function AutoRituals({ onMorning, onReview }: { onMorning: () => void; onReview: () => void }) {
  const { settings, saveSettings, loading } = useStore()

  useEffect(() => {
    if (loading) return
    const today = todayStr()
    // 延后一拍再弹，避免与首屏数据加载抢占注意力。
    const timer = window.setTimeout(() => {
      const shownToday = (key: string) => {
        try {
          return localStorage.getItem(key) === today
        } catch {
          return false
        }
      }
      const markShown = (key: string) => {
        try {
          localStorage.setItem(key, today)
        } catch {
          /* 隐私模式等场景下静默跳过，仅依赖服务端记录 */
        }
      }
      if (settings.morningPlanDone !== today && !shownToday('shenshi.morningPlanShown')) {
        onMorning()
        markShown('shenshi.morningPlanShown')
        void saveSettings({ morningPlanDone: today })
        return
      }
      if (
        settings.reviewDone !== today &&
        !shownToday('shenshi.reviewShown') &&
        new Date().getHours() >= 20
      ) {
        onReview()
        markShown('shenshi.reviewShown')
      }
    }, 900)
    return () => window.clearTimeout(timer)
    // 仅在加载完成后触发一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading])

  return null
}

/* ---------------- 晨省 ---------------- */

function MorningPlan({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { lists, todayFocusIds, setTodayFocus, moveTask, updateTask, toggleTask, saveSettings } = useStore()
  const [overdue, setOverdue] = useState<Task[]>([])
  const [today, setToday] = useState<Task[]>([])
  const [inbox, setInbox] = useState<Task[]>([])
  const [picked, setPicked] = useState<number[]>(todayFocusIds)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setPicked(todayFocusIds)
    void Promise.all([
      api.listTasks({ smart: 'overdue' }),
      api.listTasks({ smart: 'today' }),
      api.listTasks({ smart: 'inbox' }).catch(() => ({ tasks: [] as Task[], count: 0 })),
    ]).then(([o, t, i]) => {
      setOverdue(o.tasks)
      setToday(t.tasks)
      setInbox(i.tasks)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const doneToday = today.filter((t) => t.status === 'done').length
  const pending = today.filter((t) => t.status !== 'done').length
  const line = morningLine(pending, overdue.length, doneToday)

  const togglePick = (id: number) => {
    setPicked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : prev.length >= 3 ? prev : [...prev, id]))
  }

  const start = async () => {
    setBusy(true)
    try {
      await setTodayFocus(picked)
      await saveSettings({ morningPlanDone: todayStr() })
      onClose()
    } finally {
      setBusy(false)
    }
  }

  const inboxOf = (id: number) => {
    const l = lists.find((x) => x.id === id)
    return l?.name ?? '收集箱'
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        <span className="inline-flex items-center gap-2">
          <IconSunrise size={17} className="text-seal" />
          晨省 · {fullDate(todayStr())}
        </span>
      }
      subtitle="先定今日之重，再谈其余"
      width={620}
      footer={
        <>
          <span className="mr-auto text-[0.71875rem] text-ink-3">
            已选 {picked.length}/3 件为今日重点
          </span>
          <Button variant="ghost" onClick={onClose}>
            稍后
          </Button>
          <Button variant="primary" icon={IconCheck} onClick={start} disabled={busy}>
            开始今日
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <div className="rounded-xl border border-seal/25 bg-seal/6 px-3.5 py-3">
          <p className="brand-serif text-[0.84375rem] leading-relaxed text-ink">{line}</p>
          <p className="mt-1 text-[0.71875rem] text-ink-3">
            {QUOTES.morning.text} —— {QUOTES.morning.source}
          </p>
        </div>

        {/* 逾期 */}
        {overdue.length > 0 ? (
          <Section
            title="逾期未了"
            count={overdue.length}
            tone="danger"
            note="逐条安排：移回今天，或承认它需要更晚。"
          >
            {overdue.map((t) => (
              <PlanRow
                key={t.id}
                task={t}
                listName={inboxOf(t.listId)}
                picked={picked.includes(t.id)}
                onPick={() => togglePick(t.id)}
                actions={
                  <>
                    <MiniButton onClick={() => void moveTask(t.id, { dueDate: todayStr() })}>移到今天</MiniButton>
                    <MiniButton onClick={() => void moveTask(t.id, { dueDate: addDays(todayStr(), 1) })}>明天</MiniButton>
                    <MiniButton onClick={() => void toggleTask(t.id)}>完成</MiniButton>
                  </>
                }
              />
            ))}
          </Section>
        ) : null}

        {/* 今日 */}
        <Section title="今日到期" count={today.filter((t) => t.status !== 'done').length} tone="accent">
          {today.filter((t) => t.status !== 'done').length === 0 ? (
            <p className="px-1 py-2 text-[0.78125rem] text-ink-3">今天没有排定的事项，或可挑一件真正要紧的来做。</p>
          ) : (
            today
              .filter((t) => t.status !== 'done')
              .map((t) => (
                <PlanRow
                  key={t.id}
                  task={t}
                  listName={inboxOf(t.listId)}
                  picked={picked.includes(t.id)}
                  onPick={() => togglePick(t.id)}
                  actions={
                    <>
                      <MiniButton onClick={() => void moveTask(t.id, { dueDate: addDays(todayStr(), 1) })}>
                        顺延
                      </MiniButton>
                      <MiniButton onClick={() => void toggleTask(t.id)}>完成</MiniButton>
                    </>
                  }
                />
              ))
          )}
        </Section>

        {/* 收集箱 */}
        {inbox.length > 0 ? (
          <Section title="收集箱待澄清" count={inbox.length} note="先决定它们属于哪一天，或哪一份清单。">
            {inbox.slice(0, 6).map((t) => (
              <PlanRow
                key={t.id}
                task={t}
                listName={inboxOf(t.listId)}
                picked={picked.includes(t.id)}
                onPick={() => togglePick(t.id)}
                actions={
                  <>
                    <MiniButton onClick={() => void moveTask(t.id, { dueDate: todayStr() })}>今天</MiniButton>
                    <MiniButton onClick={() => void moveTask(t.id, { dueDate: addDays(todayStr(), 1) })}>明天</MiniButton>
                    <select
                      className="h-6 rounded-md border border-line bg-surface px-1 text-[0.71875rem]"
                      defaultValue=""
                      onChange={(e) => {
                        if (!e.target.value) return
                        void updateTask(t.id, { listId: Number(e.target.value) })
                        e.currentTarget.value = ''
                      }}
                    >
                      <option value="">移入清单…</option>
                      {lists.map((l) => (
                        <option key={l.id} value={l.id}>
                          {l.name}
                        </option>
                      ))}
                    </select>
                  </>
                }
              />
            ))}
            {inbox.length > 6 ? (
              <p className="px-1 pt-1 text-[0.71875rem] text-ink-3">另有 {inbox.length - 6} 项，稍后处理。</p>
            ) : null}
          </Section>
        ) : null}

        {overdue.length === 0 && pending === 0 && inbox.length === 0 ? (
          <p className="py-6 text-center text-[0.78125rem] text-ink-3">
            今日一片空白。空白也是一种安排 —— 若真要添一件，现在就是最好的时机。
          </p>
        ) : null}
      </div>
    </Modal>
  )
}

function Section({
  title,
  count,
  note,
  tone,
  children,
}: {
  title: string
  count?: number
  note?: string
  tone?: 'danger' | 'accent'
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-1.5 flex items-center gap-2">
        <h3
          className={cx(
            'text-[0.8125rem] font-semibold',
            tone === 'danger' ? 'text-p-high' : tone === 'accent' ? 'text-seal' : 'text-ink',
          )}
        >
          {title}
        </h3>
        {count !== undefined ? (
          <span className="rounded-full bg-surface-2 px-1.5 text-[0.6875rem] text-ink-3 tabular-nums">{count}</span>
        ) : null}
      </div>
      {note ? <p className="mb-1.5 text-[0.71875rem] text-ink-3">{note}</p> : null}
      <div className="space-y-1">{children}</div>
    </section>
  )
}

function PlanRow({
  task,
  listName,
  picked,
  onPick,
  actions,
}: {
  task: Task
  listName: string
  picked: boolean
  onPick: () => void
  actions: React.ReactNode
}) {
  return (
    <div
      className={cx(
        'flex flex-wrap items-center gap-2 rounded-xl border px-2.5 py-2 transition-colors',
        picked ? 'border-seal/40 bg-seal/8' : 'border-line bg-surface hover:border-line-strong',
      )}
    >
      <RoundCheck checked={picked} onChange={onPick} color="var(--seal)" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[0.8125rem] text-ink">{task.title}</div>
        <div className="text-[0.6875rem] text-ink-3">
          {listName}
          {task.dueDate ? ` · ${task.dueDate}` : ''}
          {task.dueTime ? ` ${task.dueTime}` : ''}
          {picked ? ' · 已列入今日三件事' : ''}
        </div>
      </div>
      <div className="flex items-center gap-1">{actions}</div>
    </div>
  )
}

function MiniButton({ children, onClick }: { children: React.ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-md border border-line px-1.5 py-0.5 text-[0.71875rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal"
    >
      {children}
    </button>
  )
}

/* ---------------- 日省 ---------------- */

const MOODS = ['稳', '顺', '疲', '滞', '躁']

function DailyReview({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { stats, loadStats, saveSettings } = useStore()
  const [date, setDate] = useState(todayStr())
  const [mood, setMood] = useState('')
  const [wins, setWins] = useState('')
  const [blockers, setBlockers] = useState('')
  const [tomorrow, setTomorrow] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    const d = todayStr()
    setDate(d)
    void loadStats(7)
    api
      .getReview(d)
      .then((r) => {
        setMood(r.mood)
        setWins(r.wins)
        setBlockers(r.blockers)
        setTomorrow(r.tomorrow)
      })
      .catch(() => undefined)
  }, [open, loadStats])

  const submit = async () => {
    setBusy(true)
    try {
      await api.saveReview({ date, mood, wins, blockers, tomorrow })
      await saveSettings({ reviewDone: todayStr() })
      onClose()
    } finally {
      setBusy(false)
    }
  }

  const done = stats?.doneToday ?? 0
  const open_ = stats ? stats.dueToday - stats.dueTodayDone : 0

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        <span className="inline-flex items-center gap-2">
          <IconBook size={17} className="text-seal" />
          日省 · {date}
        </span>
      }
      subtitle="善始者众，克终者寡 —— 花两分钟把今天收好"
      width={560}
      footer={
        <>
          <span className="mr-auto text-[0.71875rem] text-ink-3">复盘只对自己可见，可随时补写或修改</span>
          <Button variant="ghost" onClick={onClose}>
            稍后
          </Button>
          <Button variant="primary" icon={IconCheck} onClick={submit} disabled={busy}>
            存下今天
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="grid grid-cols-3 gap-3">
          <Stat label="今日完成" value={done} tone="accent" />
          <Stat label="尚未了结" value={Math.max(0, open_)} />
          <Stat label="连续完成" value={`${stats?.streakDays ?? 0} 天`} />
        </div>

        <p className="brand-serif rounded-xl border border-line bg-surface-2/50 px-3.5 py-2.5 text-[0.8125rem] leading-relaxed text-ink-2">
          {reviewLine(done, Math.max(0, open_))}
        </p>

        <div>
          <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">今日状态</span>
          <div className="flex flex-wrap gap-1.5">
            {MOODS.map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => setMood(mood === m ? '' : m)}
                className={cx(
                  'brand-serif h-9 w-9 rounded-lg border text-[0.875rem] transition-colors',
                  mood === m ? 'border-seal bg-seal/10 text-seal' : 'border-line text-ink-2 hover:bg-surface-2',
                )}
              >
                {m}
              </button>
            ))}
          </div>
        </div>

        <ReviewField
          label="今日成了什么"
          value={wins}
          onChange={setWins}
          placeholder="哪怕只是一件小事，写下来才算真的完成。"
        />
        <ReviewField
          label="什么阻碍了进度"
          value={blockers}
          onChange={setBlockers}
          placeholder="写清楚卡在哪里，明天才不必重新摸索。"
        />
        <ReviewField
          label="明日的首要之事"
          value={tomorrow}
          onChange={setTomorrow}
          placeholder="只写一件 —— 慎始的诀窍在于收敛。"
        />
      </div>
    </Modal>
  )
}

function Stat({ label, value, tone }: { label: string; value: string | number; tone?: 'accent' }) {
  return (
    <div className="rounded-xl border border-line bg-surface px-3 py-2">
      <div className="text-[0.6875rem] text-ink-3">{label}</div>
      <div className={cx('text-[1.125rem] font-semibold tabular-nums', tone === 'accent' ? 'text-seal' : 'text-ink')}>
        {value}
      </div>
    </div>
  )
}

function ReviewField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">{label}</span>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        rows={2}
        className={cx(inputClass, 'resize-none leading-6')}
      />
    </label>
  )
}

/* ---------------- 专注 ---------------- */

const FOCUS_PRESETS = [
  { minutes: 25, label: '25 分钟 · 一番茄' },
  { minutes: 45, label: '45 分钟 · 深度一段' },
  { minutes: 15, label: '15 分钟 · 启动一下' },
]

function FocusPanel({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { focus, startFocus, stopFocus, tasks } = useStore()
  const [taskId, setTaskId] = useState<number | ''>('')
  const [minutes, setMinutes] = useState(25)

  useEffect(() => {
    if (open && focus.taskId) setTaskId(focus.taskId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const mm = String(Math.floor(focus.remaining / 60)).padStart(2, '0')
  const ss = String(focus.remaining % 60).padStart(2, '0')
  const progress = focus.running ? 1 - focus.remaining / (focus.minutes * 60) : 0
  const current = tasks.find((t) => t.id === focus.taskId) ?? tasks.find((t) => t.id === taskId)

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        <span className="inline-flex items-center gap-2">
          <IconTimer size={17} className="text-seal" />
          专注
        </span>
      }
      subtitle={QUOTES.focus.text + ' —— ' + QUOTES.focus.source}
      width={420}
      footer={
        focus.running ? (
          <>
            <Button variant="ghost" onClick={() => void stopFocus(false)}>
              放弃本次
            </Button>
            <Button variant="primary" icon={IconCheck} onClick={() => void stopFocus(true)}>
              提前结束并记录
            </Button>
          </>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              关闭
            </Button>
            <Button
              variant="primary"
              icon={IconTimer}
              onClick={() => startFocus(typeof taskId === 'number' ? taskId : null, minutes)}
            >
              开始专注
            </Button>
          </>
        )
      }
    >
      {focus.running ? (
        <div className="flex flex-col items-center gap-3 py-2">
          <ProgressRing value={progress} size={148} stroke={8}>
            <div className="text-center">
              <div className="brand-serif text-[1.875rem] font-semibold tabular-nums leading-none text-ink">
                {mm}:{ss}
              </div>
              <div className="mt-1 text-[0.6875rem] text-ink-3">共 {focus.minutes} 分钟</div>
            </div>
          </ProgressRing>
          <p className="max-w-[16rem] text-center text-[0.78125rem] leading-relaxed text-ink-2">
            {current ? `正在专注：${current.title}` : '未关联任务，专注时间仍会记录。'}
          </p>
          <p className="text-[0.71875rem] text-ink-3">倒计时结束会自动记录时长并提示。</p>
        </div>
      ) : (
        <div className="space-y-4">
          <div>
            <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">专注时长</span>
            <div className="space-y-1.5">
              {FOCUS_PRESETS.map((p) => (
                <button
                  key={p.minutes}
                  type="button"
                  onClick={() => setMinutes(p.minutes)}
                  className={cx(
                    'flex w-full items-center justify-between rounded-lg border px-3 py-2 text-[0.8125rem] transition-colors',
                    minutes === p.minutes ? 'border-seal bg-seal/10 text-seal' : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                >
                  {p.label}
                  {minutes === p.minutes ? <IconCheck size={14} /> : null}
                </button>
              ))}
            </div>
          </div>
          <div>
            <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">
              关联任务（可选）
            </span>
            <select
              className={inputClass}
              value={taskId}
              onChange={(e) => setTaskId(e.target.value ? Number(e.target.value) : '')}
            >
              <option value="">不关联</option>
              {tasks
                .filter((t) => t.status !== 'done')
                .slice(0, 60)
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.title}
                  </option>
                ))}
            </select>
          </div>
        </div>
      )}
    </Modal>
  )
}

/* ---------------- 提醒中心 ---------------- */

function ReminderCenter() {
  const { reminders, dismissReminder, snoozeReminder, toggleTask, setSelectedTask, setView, select } = useStore()
  // 权限状态跟着浏览器实时走：用户在站点设置里改过之后，这里的文案不会再停留在旧值。
  const { diag, request } = useNotifyDiagnosis()
  const [collapsed, setCollapsed] = useState(false)

  if (reminders.length === 0) return null

  return (
    <div className="fixed bottom-4 right-4 z-40 w-[330px] animate-rise">
      <div className="overflow-hidden rounded-2xl border border-line bg-surface shadow-[var(--shadow-lg)]">
        <header className="flex items-center gap-2 border-b border-line bg-seal/8 px-3 py-2">
          <IconBell size={15} className="text-seal" />
          <span className="brand-serif flex-1 text-[0.8125rem] font-semibold text-ink">提醒 · {reminders.length} 条</span>
          <button
            type="button"
            onClick={() => setCollapsed((v) => !v)}
            className="text-[0.71875rem] text-ink-3 transition-colors hover:text-ink"
          >
            {collapsed ? '展开' : '收起'}
          </button>
        </header>

        {!collapsed ? (
          <div className="max-h-[320px] space-y-1 overflow-y-auto p-2">
            {reminders.map((h) => {
              const key = `${h.task.id}|${h.fireAt}`
              return (
                <div key={key} className="rounded-xl border border-line bg-surface-2/40 px-2.5 py-2">
                  <div className="flex items-start gap-2">
                    <RoundCheck checked={false} onChange={() => {
                      void toggleTask(h.task.id)
                      dismissReminder(key)
                    }} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[0.8125rem] text-ink">{h.task.title}</div>
                      <div className="flex items-center gap-1.5 text-[0.6875rem] text-ink-3">
                        <IconClock size={10.5} />
                        {h.dueLabel}
                        {h.overdue ? <span className="text-p-high">· 已过时</span> : null}
                      </div>
                    </div>
                  </div>
                  <div className="mt-1.5 flex items-center gap-1 pl-[26px]">
                    <MiniButton
                      onClick={() => {
                        select({ kind: 'smart', key: 'all' })
                        setView('list')
                        setSelectedTask(h.task.id)
                        dismissReminder(key)
                      }}
                    >
                      查看
                    </MiniButton>
                    <MiniButton onClick={() => snoozeReminder(h, 5)}>5 分钟后</MiniButton>
                    <MiniButton onClick={() => snoozeReminder(h, 30)}>30 分钟后</MiniButton>
                    <button
                      type="button"
                      onClick={() => dismissReminder(key)}
                      className="ml-auto text-ink-3 transition-colors hover:text-ink"
                      aria-label="关闭提醒"
                    >
                      <IconX size={13} />
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        ) : null}

        {diag.state !== 'granted' ? (
          <div className="flex items-center gap-2 border-t border-line px-3 py-2">
            <IconInfo size={12} className="shrink-0 text-ink-3" />
            <span className="flex-1 text-[0.6875rem] leading-relaxed text-ink-3">
              {diag.state === 'default'
                ? '开启桌面通知后，即使切到其他窗口也不会错过提醒。'
                : `桌面通知未开启：${diag.label}`}
            </span>
            {diag.state === 'default' ? (
              <Button variant="outline" size="sm" onClick={() => void request()}>
                开启
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}

/* ---------------- 确认框 ---------------- */

function ConfirmHost() {
  const { confirmState, resolveConfirm } = useStore()
  if (!confirmState) return null
  return (
    <Modal
      open
      onClose={() => resolveConfirm(false)}
      title={confirmState.title}
      width={400}
      footer={
        <>
          <Button variant="ghost" onClick={() => resolveConfirm(false)}>
            取消
          </Button>
          <Button
            variant={confirmState.danger ? 'danger' : 'primary'}
            onClick={() => resolveConfirm(true)}
          >
            {confirmState.confirmText}
          </Button>
        </>
      }
    >
      <p className="text-[0.8125rem] leading-relaxed text-ink-2">{confirmState.message}</p>
    </Modal>
  )
}

/* ---------------- 轻提示 ---------------- */

function ToastHost() {
  const { toasts, dismissToast } = useStore()
  if (toasts.length === 0) return null
  return (
    <div className="pointer-events-none fixed left-1/2 top-4 z-50 flex -translate-x-1/2 flex-col items-center gap-2">
      {toasts.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => dismissToast(t.id)}
          className={cx(
            'pointer-events-auto flex max-w-[420px] animate-rise items-center gap-2 rounded-xl border px-3 py-2 text-left text-[0.78125rem] shadow-[var(--shadow-md)] backdrop-blur',
            t.kind === 'error'
              ? 'border-p-high/30 bg-p-high/10 text-ink'
              : t.kind === 'info'
                ? 'border-line bg-surface/95 text-ink-2'
                : 'border-seal/25 bg-surface/95 text-ink',
          )}
        >
          {t.kind === 'error' ? (
            <IconAlert size={14} className="shrink-0 text-p-high" />
          ) : (
            <IconCheck size={14} className="shrink-0 text-seal" />
          )}
          {t.message}
        </button>
      ))}
    </div>
  )
}

