import { useMemo, useState } from 'react'

import { footnoteOfTheDay } from '../lib/quotes'
import { fullDate, greeting, todayStr } from '../lib/date'
import { describeFilter, isFilterActive, applyFilter, type TaskFilter } from '../lib/filter'
import { useStore } from '../store/AppStore'
import type { TaskSort } from '../api/client'
import type { SmartKey, ViewKind } from '../types'
import {
  IconChart,
  IconCheck,
  IconColumns,
  IconFlag,
  IconGrid,
  IconList,
  IconSeedling,
  IconSort,
  IconSparkle,
  IconStar,
  IconX,
} from './icons'
import { SearchBar } from './TaskViews'
import { IconButton, Popover, cx } from './ui'

const VIEW_TABS: { key: ViewKind; label: string; icon: typeof IconList }[] = [
  { key: 'list', label: '列表', icon: IconList },
  { key: 'board', label: '看板', icon: IconColumns },
  { key: 'calendar', label: '日历', icon: IconGrid },
  { key: 'quadrant', label: '四象限', icon: IconSparkle },
  { key: 'habits', label: '习惯', icon: IconSeedling },
  { key: 'stats', label: '统计', icon: IconChart },
]

const SORT_OPTIONS: { value: TaskSort; label: string; hint: string }[] = [
  { value: 'smart', label: '智能', hint: '到期与优先级优先' },
  { value: 'manual', label: '手动', hint: '拖动任务行调整' },
  { value: 'priority', label: '优先级', hint: '高在前' },
  { value: 'due', label: '到期时间', hint: '近在前' },
  { value: 'created', label: '创建时间', hint: '新在前' },
  { value: 'title', label: '标题', hint: '按字序' },
]

const SMART_SUBTITLE: Record<SmartKey, string> = {
  inbox: '未定归属的想法，先放在这里',
  today: '今日事，今日毕',
  next7: '未来七天的安排',
  overdue: '慎终如始，则无败事',
  nodate: '尚未安排时间的事项',
  all: '全部未完成与已完成的任务',
  done: '完成即是敬终',
}

export function Toolbar({ filters, onFilters }: { filters: TaskFilter; onFilters: (f: TaskFilter) => void }) {
  const { selection, view, setView, tasks, multiSelect, setMultiSelect, lists, tags, keyword, sortBy, setSortBy } =
    useStore()
  const [filterOpen, setFilterOpen] = useState(false)
  const [sortOpen, setSortOpen] = useState(false)
  const priority = filters.priority
  const tagId = filters.tagId
  const setPriority = (v: number | null) => onFilters({ ...filters, priority: v })
  const setTagId = (v: number | null) => onFilters({ ...filters, tagId: v })

  const title = useMemo(() => {
    if (keyword.trim()) return `搜索「${keyword.trim()}」`
    if (view === 'board') return '看板'
    if (view === 'calendar') return '日历'
    if (view === 'quadrant') return '四象限'
    if (view === 'habits') return '习惯打卡'
    if (view === 'stats') return '统计与复盘'
    if (selection.kind === 'smart') {
      const map: Record<SmartKey, string> = {
        inbox: '收集箱',
        today: '今天',
        next7: '最近 7 天',
        overdue: '逾期',
        nodate: '无日期',
        all: '全部任务',
        done: '已完成',
      }
      return map[selection.key]
    }
    if (selection.kind === 'list') return lists.find((l) => l.id === selection.id)?.name ?? '清单'
    if (selection.kind === 'folder') return '分组'
    if (selection.kind === 'tag') return `#${tags.find((t) => t.id === selection.id)?.name ?? ''}`
    return '搜索结果'
  }, [selection, view, lists, tags, keyword])

  const subtitle = useMemo(() => {
    if (keyword.trim()) return '在标题与备注中查找'
    if (view === 'calendar') return '按月或按周查看安排，可拖动任务改期'
    if (view === 'quadrant') return '按重要与紧急程度分配精力'
    if (view === 'board') return '横向铺开，按维度分组查看'
    if (view === 'habits')
      return '日日不断之功，连续与积累都记在这张轨迹上'
    if (view === 'stats') return '数据与复盘，日计不足，岁计有余'
    if (selection.kind === 'smart') return SMART_SUBTITLE[selection.key]
    if (selection.kind === 'list') {
      const l = lists.find((x) => x.id === selection.id)
      return l?.folderId ? '清单 · 已归入分组' : '独立清单'
    }
    if (selection.kind === 'tag') return '按标签横向串联的任务'
    return ''
  }, [selection, view, lists, keyword])

  const openCount = tasks.filter((t) => t.status === 'todo').length
  const doneCount = tasks.filter((t) => t.status === 'done').length
  const hitCount = useMemo(() => applyFilter(tasks, filters).length, [tasks, filters])

  const footnote = footnoteOfTheDay()

  return (
    <header className="relative z-30 shrink-0 border-b border-line bg-paper/85 px-5 pt-3.5 backdrop-blur">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="brand-serif truncate text-[21px] font-semibold leading-7 text-ink">{title}</h1>
          <p className="mt-0.5 flex flex-wrap items-center gap-2 text-[11.5px] text-ink-3">
            <span>{subtitle}</span>
            {view === 'list' || view === 'board' ? (
              <span className="text-ink-3/80">
                · 待办 {openCount}
                {doneCount ? ` · 已完成 ${doneCount}` : ''}
              </span>
            ) : null}
            <span className="hidden xl:inline">· {fullDate(todayStr())} · {greeting()}</span>
          </p>
        </div>

        <div className="flex items-center gap-2">
          <SearchBar />

          <div className="flex rounded-lg border border-line bg-surface p-0.5">
            {VIEW_TABS.map((v) => (
              <button
                key={v.key}
                type="button"
                title={v.label}
                onClick={() => setView(v.key)}
                className={cx(
                  'inline-flex items-center gap-1 rounded-md px-2 py-1 text-[12.5px] transition-colors',
                  view === v.key ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:text-ink',
                )}
              >
                <v.icon size={13} />
                <span className="hidden lg:inline">{v.label}</span>
              </button>
            ))}
          </div>

          <div className="relative">
            <IconButton
              icon={IconFlag}
              label="筛选"
              active={priority !== null || tagId !== null}
              onClick={() => setFilterOpen((v) => !v)}
            />
            <Popover open={filterOpen} onClose={() => setFilterOpen(false)} align="right" width={230}>
              <div className="p-1.5">
                <div className="px-1 pb-1 text-[11px] tracking-wide text-ink-3">优先级</div>
                <div className="flex gap-1">
                  {[
                    { v: null, l: '全部' },
                    { v: 3, l: '高' },
                    { v: 2, l: '中' },
                    { v: 1, l: '低' },
                    { v: 0, l: '无' },
                  ].map((o) => (
                    <button
                      key={String(o.v)}
                      type="button"
                      onClick={() => setPriority(o.v)}
                      className={cx(
                        'flex-1 rounded-md py-1 text-[12px] transition-colors',
                        priority === o.v ? 'bg-seal/12 font-medium text-seal' : 'text-ink-2 hover:bg-surface-2',
                      )}
                    >
                      {o.l}
                    </button>
                  ))}
                </div>

                {tags.length > 0 ? (
                  <>
                    <div className="px-1 pb-1 pt-2.5 text-[11px] tracking-wide text-ink-3">标签</div>
                    <div className="max-h-44 overflow-y-auto">
                      <button
                        type="button"
                        onClick={() => setTagId(null)}
                        className={cx(
                          'flex w-full items-center rounded-lg px-2 py-1 text-left text-[12.5px]',
                          tagId === null ? 'bg-seal/10 text-seal' : 'text-ink-2 hover:bg-surface-2',
                        )}
                      >
                        不限标签
                      </button>
                      {tags.map((t) => (
                        <button
                          key={t.id}
                          type="button"
                          onClick={() => setTagId(t.id)}
                          className={cx(
                            'flex w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-[12.5px]',
                            tagId === t.id ? 'bg-seal/10 text-seal' : 'text-ink-2 hover:bg-surface-2',
                          )}
                        >
                          <span className="h-2 w-2 rounded-full" style={{ background: t.color }} />
                          {t.name}
                        </button>
                      ))}
                    </div>
                  </>
                ) : null}
              </div>
            </Popover>
          </div>

          {view === 'list' ? (
            <div className="relative">
              <IconButton
                icon={IconSort}
                label="排序方式"
                active={sortBy !== 'smart'}
                onClick={() => setSortOpen((v) => !v)}
              />
              <Popover open={sortOpen} onClose={() => setSortOpen(false)} align="right" width={232}>
                <div className="p-1.5">
                  <div className="px-1 pb-1 text-[11px] tracking-wide text-ink-3">排序方式</div>
                  {SORT_OPTIONS.map((o) => (
                    <button
                      key={o.value}
                      type="button"
                      onClick={() => {
                        setSortBy(o.value)
                        setSortOpen(false)
                      }}
                      className={cx(
                        'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[12.5px] transition-colors',
                        sortBy === o.value ? 'bg-seal/10 text-seal' : 'text-ink hover:bg-surface-2',
                      )}
                    >
                      <span className="flex-1">{o.label}</span>
                      <span className="text-[10.5px] text-ink-3">{o.hint}</span>
                      {sortBy === o.value ? <IconCheck size={12} /> : null}
                    </button>
                  ))}
                  {sortBy === 'manual' ? (
                    <p className="border-t border-line px-2 pb-1 pt-2 text-[11px] leading-relaxed text-ink-3">
                      按住任务行左缘的手柄上下拖动即可调整顺序。
                    </p>
                  ) : null}
                </div>
              </Popover>
            </div>
          ) : null}

          <IconButton
            icon={IconCheck}
            label={multiSelect ? '退出多选' : '多选'}
            active={multiSelect}
            onClick={() => setMultiSelect(!multiSelect)}
          />
        </div>
      </div>

      {/* 今日三件事 */}
      {view === 'list' && selection.kind === 'smart' && (selection.key === 'today' || selection.key === 'all') ? (
        <TodayFocusStrip />
      ) : null}

      {/* 筛选生效提示 */}
      {isFilterActive(filters) ? (
        <div className="flex items-center gap-2 pb-2 pt-2">
          <span className="text-[11.5px] text-ink-3">
            已筛选：{describeFilter(filters, tags)} · 命中 {hitCount} 项
          </span>
          <button
            type="button"
            onClick={() => onFilters({ priority: null, tagId: null })}
            className="inline-flex items-center gap-1 text-[11.5px] text-seal hover:underline"
          >
            <IconX size={11} />
            清除
          </button>
        </div>
      ) : (
        <div className="flex items-center justify-end pb-1.5 pt-1 text-[10.5px] text-ink-3/70">
          {footnote.text} · {footnote.source}
        </div>
      )}
    </header>
  )
}

/** 今日三件事：晨省选的三个重点，摆在今天视图最上方。 */
function TodayFocusStrip() {
  const { todayFocusIds, tasks, toggleTask, setTodayFocus } = useStore()
  const focusTasks = todayFocusIds.map((id) => tasks.find((t) => t.id === id)).filter(Boolean)
  if (focusTasks.length === 0) {
    return (
      <div className="mb-2 flex items-center gap-2 rounded-xl border border-dashed border-line px-3 py-2">
        <IconStar size={13} className="text-ink-3" />
        <span className="text-[11.5px] text-ink-3">
          还没有选定今日重点。点左下「晨省 · 规划今日」挑出三件最要紧的事。
        </span>
      </div>
    )
  }
  const done = focusTasks.filter((t) => t!.status === 'done').length
  return (
    <div className="mb-2 flex flex-wrap items-center gap-2 rounded-xl border border-seal/25 bg-seal/6 px-3 py-2">
      <span className="brand-serif inline-flex items-center gap-1.5 text-[12.5px] font-medium text-seal">
        <IconStar size={13} />
        今日三件事
      </span>
      {focusTasks.map((t) => (
        <button
          key={t!.id}
          type="button"
          onClick={() => void toggleTask(t!.id)}
          className={cx(
            'inline-flex max-w-[220px] items-center gap-1.5 rounded-lg border px-2 py-0.5 text-[12px] transition-colors',
            t!.status === 'done'
              ? 'border-jade/30 bg-jade/10 text-ink-3 line-through'
              : 'border-line bg-surface text-ink hover:border-seal/40',
          )}
          title="点击切换完成状态"
        >
          <span className="truncate">{t!.title}</span>
        </button>
      ))}
      <span className="ml-auto text-[11px] text-ink-3">
        {done}/{focusTasks.length} 已了
      </span>
      <button
        type="button"
        onClick={() => void setTodayFocus([])}
        className="text-[11px] text-ink-3 transition-colors hover:text-ink"
      >
        清除
      </button>
    </div>
  )
}
