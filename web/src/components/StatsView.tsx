import { useEffect, useMemo, useState } from 'react'

import { api } from '../api/client'
import { todayStr } from '../lib/date'
import { QUOTES, reviewLine } from '../lib/quotes'
import { useStore } from '../store/AppStore'
import type { CountByKey, Review, Stats } from '../types'
import { IconBook, IconChart, IconRepeat, IconSparkle, IconTimer } from './icons'
import { ProgressRing, cx } from './ui'

const RANGES = [
  { days: 14, label: '近 14 天' },
  { days: 30, label: '近 30 天' },
  { days: 90, label: '近 90 天' },
]

export function StatsView() {
  const { stats, statsError, loadStats, version } = useStore()
  const [days, setDays] = useState(30)
  const [reviews, setReviews] = useState<Review[]>([])
  const [focusTotal, setFocusTotal] = useState(0)

  useEffect(() => {
    void loadStats(days)
  }, [days, loadStats, version])

  useEffect(() => {
    api
      .listReviews(12)
      .then((r) => setReviews(r.reviews))
      .catch(() => undefined)
    api
      .listFocus(200)
      .then((r) => setFocusTotal(r.sessions.reduce((sum, s) => sum + s.minutes, 0)))
      .catch(() => undefined)
  }, [version])

  const chart = useMemo(() => {
    if (!stats) return { rows: [], max: 1 }
    const rows = stats.trend.slice(-days)
    const max = Math.max(1, ...rows.map((r) => Math.max(r.created, r.done)))
    return { rows, max }
  }, [stats, days])

  if (statsError && !stats) {
    // 接口挂了就明说「加载失败」，别把故障渲染成「正在统计…」再永久卡住。
    return (
      <div className="grid h-full place-items-center text-center">
        <div className="space-y-2">
          <p className="text-[0.8125rem] text-ink-2">统计加载失败</p>
          <p className="text-[0.71875rem] text-ink-3">请检查服务是否在运行，或稍后重试。</p>
          <button
            type="button"
            onClick={() => void loadStats(days)}
            className="rounded-lg border border-line px-3 py-1.5 text-[0.78125rem] text-ink-2 transition-colors hover:bg-surface-2"
          >
            重试
          </button>
        </div>
      </div>
    )
  }

  if (!stats) {
    return (
      <div className="grid h-full place-items-center text-[0.8125rem] text-ink-3">
        <span>正在统计…</span>
      </div>
    )
  }

  const todayDone = stats.doneToday
  const openToday = stats.dueToday - stats.dueTodayDone

  return (
    <div className="h-full overflow-y-auto px-5 py-4">
      {/* 概览 */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi
          label="未完成"
          value={stats.totalOpen}
          hint={`其中今日到期 ${stats.dueToday} 件`}
          tone={stats.overdue > 0 ? 'danger' : 'normal'}
        />
        <Kpi label="今日完成" value={todayDone} hint={`待办还剩 ${Math.max(0, openToday)} 件`} tone="accent" />
        <Kpi
          label="连续完成"
          value={`${stats.streakDays} 天`}
          hint={stats.streakDays >= 3 ? '节奏在延续' : '慎终如始，则无败事'}
        />
        <Kpi
          label="累计专注"
          value={`${Math.round(focusTotal / 60)} 时`}
          hint={`近 ${days} 天 ${stats.focusMinutes} 分钟`}
        />
      </div>

      {/* 完成率与结语 */}
      <div className="mt-3 flex flex-col gap-3 rounded-2xl border border-line bg-surface/60 p-4 sm:flex-row sm:items-center">
        <ProgressRing value={stats.completion / 100} size={72} stroke={5}>
          <span className="text-[0.8125rem] font-medium tabular-nums text-ink">{stats.completion.toFixed(0)}%</span>
        </ProgressRing>
        <div className="min-w-0 flex-1">
          <div className="brand-serif text-[0.875rem] text-ink">全部完成率</div>
          <p className="mt-1 text-[0.78125rem] leading-relaxed text-ink-2">{reviewLine(todayDone, Math.max(0, openToday))}</p>
          <p className="mt-1 text-[0.71875rem] text-ink-3">
            累计 {stats.totalAll} 件 · 已完成 {stats.totalDone} 件 · 逾期 {stats.overdue} 件
          </p>
        </div>
        {stats.overdue > 0 ? (
          <div className="rounded-xl border border-p-high/25 bg-p-high/8 px-3 py-2 text-[0.75rem] text-ink-2 sm:max-w-[220px]">
            <span className="font-medium text-p-high">有 {stats.overdue} 件逾期</span>
            <span className="mt-0.5 block leading-relaxed">先安顿它们，再向前排新的计划。</span>
          </div>
        ) : null}
      </div>

      {/* 趋势 */}
      <section className="mt-4 rounded-2xl border border-line bg-surface/60 p-4">
        <header className="mb-3 flex flex-wrap items-center gap-2">
          <IconChart size={15} className="text-seal" />
          <h3 className="brand-serif text-[0.875rem] font-semibold text-ink">新建与完成趋势</h3>
          <div className="ml-auto flex rounded-lg border border-line p-0.5">
            {RANGES.map((r) => (
              <button
                key={r.days}
                type="button"
                onClick={() => setDays(r.days)}
                className={cx(
                  'rounded-md px-2.5 py-1 text-[0.75rem] transition-colors',
                  days === r.days ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
                )}
              >
                {r.label}
              </button>
            ))}
          </div>
        </header>

        <div className="flex items-center gap-4 text-[0.71875rem] text-ink-3">
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm" style={{ background: 'var(--seal)' }} />
            完成
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm" style={{ background: 'var(--line-strong)' }} />
            新建
          </span>
        </div>

        <div className="mt-3 flex h-[150px] items-end gap-[3px] overflow-x-auto">
          {chart.rows.map((r) => {
            const doneH = (r.done / chart.max) * 100
            const createdH = (r.created / chart.max) * 100
            return (
              <div
                key={r.date}
                className="group/bar flex min-w-[8px] flex-1 flex-col items-center justify-end gap-0.5"
                title={`${r.date}\n完成 ${r.done} · 新建 ${r.created}${r.focus ? ` · 专注 ${r.focus} 分钟` : ''}`}
              >
                <div className="flex h-full w-full items-end justify-center gap-[1px]">
                  <span
                    className="w-1/2 rounded-t-[3px] bg-seal/85 transition-all group-hover/bar:bg-seal"
                    style={{ height: `${Math.max(doneH, r.done ? 3 : 0)}%` }}
                  />
                  <span
                    className="w-1/2 rounded-t-[3px] bg-line-strong transition-all"
                    style={{ height: `${Math.max(createdH, r.created ? 3 : 0)}%` }}
                  />
                </div>
                <span className="truncate text-[0.5625rem] tabular-nums text-ink-3">
                  {days <= 30 ? r.date.slice(8) : ''}
                </span>
              </div>
            )
          })}
        </div>
        <p className="mt-2 text-[0.6875rem] text-ink-3">横轴为日期，纵轴为条数；悬停可看当日明细。</p>
      </section>

      {/* 分布 */}
      <div className="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-3">
        <Distribution title="按清单" icon={IconChart} rows={stats.byList} emptyText="暂无未完成任务" />
        <Distribution title="按优先级" icon={IconSparkle} rows={stats.byPriority} emptyText="暂无未完成任务" />
        <Distribution title="按四象限" icon={IconSparkle} rows={stats.byQuadrant} emptyText="暂无未完成任务" />
      </div>

      {/* 复盘 */}
      <section className="mt-4 mb-8 rounded-2xl border border-line bg-surface/60 p-4">
        <header className="mb-3 flex items-center gap-2">
          <IconBook size={15} className="text-seal" />
          <h3 className="brand-serif text-[0.875rem] font-semibold text-ink">日省记录</h3>
          <span className="text-[0.71875rem] text-ink-3">{QUOTES.review.text}</span>
          <button
            type="button"
            onClick={() => window.dispatchEvent(new CustomEvent('shenshi:review'))}
            className="ml-auto rounded-lg border border-line px-2.5 py-1 text-[0.75rem] text-ink-2 transition-colors hover:bg-surface-2"
          >
            写今日复盘
          </button>
        </header>

        {reviews.length === 0 ? (
          <p className="py-6 text-center text-[0.78125rem] text-ink-3">
            还没有复盘记录。每天晚上花两分钟回看今天，是「敬终」最轻的一种练习。
          </p>
        ) : (
          <ul className="space-y-2">
            {reviews.map((r) => (
              <li key={r.id} className="rounded-xl border border-line bg-surface-2/40 px-3 py-2.5">
                <div className="flex items-center gap-2">
                  <span className="text-[0.78125rem] font-medium text-ink tabular-nums">{r.date}</span>
                  {r.mood ? (
                    <span className="rounded-md bg-seal/10 px-1.5 py-0.5 text-[0.71875rem] text-seal">{r.mood}</span>
                  ) : null}
                  {r.date === todayStr() ? <span className="text-[0.6875rem] text-ink-3">今日</span> : null}
                </div>
                <div className="mt-1 space-y-0.5 text-[0.78125rem] leading-relaxed text-ink-2">
                  {r.wins ? <p>成：{r.wins}</p> : null}
                  {r.blockers ? <p>阻：{r.blockers}</p> : null}
                  {r.tomorrow ? <p>明日首要：{r.tomorrow}</p> : null}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <div className="pointer-events-none fixed bottom-3 right-4 flex items-center gap-1 text-[0.65625rem] text-ink-3/70">
        <IconTimer size={11} />
        {stats.focusMinutes} 分钟专注 · 近 {days} 天
        <IconRepeat size={11} className="ml-2" />
        {stats.streakDays} 天连续
      </div>
    </div>
  )
}

function Kpi({
  label,
  value,
  hint,
  tone = 'normal',
}: {
  label: string
  value: string | number
  hint?: string
  tone?: 'normal' | 'accent' | 'danger'
}) {
  return (
    <div className="rounded-2xl border border-line bg-surface/60 px-3.5 py-3">
      <div className="text-[0.71875rem] tracking-wide text-ink-3">{label}</div>
      <div
        className={cx(
          'mt-0.5 text-[1.375rem] font-semibold leading-8 tabular-nums',
          tone === 'danger' ? 'text-p-high' : tone === 'accent' ? 'text-seal' : 'text-ink',
        )}
      >
        {value}
      </div>
      {hint ? <div className="text-[0.71875rem] text-ink-3">{hint}</div> : null}
    </div>
  )
}

function Distribution({
  title,
  icon: Icon,
  rows,
  emptyText,
}: {
  title: string
  icon: (p: { size?: number; className?: string }) => React.ReactNode
  rows: CountByKey[]
  emptyText: string
}) {
  const max = Math.max(1, ...rows.map((r) => r.count))
  return (
    <section className="rounded-2xl border border-line bg-surface/60 p-4">
      <header className="mb-3 flex items-center gap-2">
        <Icon size={14} className="text-ink-3" />
        <h3 className="text-[0.8125rem] font-medium text-ink">{title}</h3>
      </header>
      {rows.length === 0 ? (
        <p className="py-4 text-[0.75rem] text-ink-3">{emptyText}</p>
      ) : (
        <ul className="space-y-2">
          {rows.map((r) => (
            <li key={r.key} className="flex items-center gap-2.5">
              <span className="w-[76px] shrink-0 truncate text-[0.75rem] text-ink-2">{r.label}</span>
              <span className="h-2 flex-1 overflow-hidden rounded-full bg-surface-2">
                <span
                  className="block h-full rounded-full bg-seal/70"
                  style={{ width: `${(r.count / max) * 100}%` }}
                />
              </span>
              <span className="w-6 shrink-0 text-right text-[0.71875rem] tabular-nums text-ink-3">{r.count}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
