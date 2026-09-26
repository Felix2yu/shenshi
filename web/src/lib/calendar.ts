/**
 * 农历与法定节假日信息。
 *
 * 数据一律来自后端 /api/meta/calendar —— 前端不自己算农历。
 * 日历显示什么、重复规则排到哪天，必须是同一份数据，否则迟早出现
 * 「日历写着放假、任务却排到那天上班」这种自相矛盾的结果。
 * 这里只做一个按日期缓存的取用层：拿过一次就记住，重复渲染不再打接口。
 */

import { useEffect, useState } from 'react'

import { api } from '../api/client'
import type { CalendarDayInfo } from '../types'

const cache = new Map<string, CalendarDayInfo>()
/** 已请求过的区间，避免同一个月来回翻页时反复打接口。 */
const requested = new Set<string>()
let officialYears: number[] = []

const listeners = new Set<() => void>()

function bump() {
  for (const l of listeners) l()
}

/** 拉取 [from, to] 并写入缓存。失败时把区间标记撤掉，下次有机会重试。 */
async function ensure(from: string, to: string): Promise<void> {
  const key = `${from}~${to}`
  if (requested.has(key)) return
  requested.add(key)
  try {
    const meta = await api.calendarMeta(from, to)
    for (const d of meta.days) cache.set(d.date, d)
    if (meta.officialYears?.length) officialYears = meta.officialYears
    bump()
  } catch {
    // 拿不到就当没有农历信息：日历照常显示公历，不打断使用。
    requested.delete(key)
  }
}

/** 同步读取某天的信息；还没拉到时返回 undefined，调用方自行降级。 */
export function getDayInfo(date: string): CalendarDayInfo | undefined {
  return cache.get(date)
}

/** 某年的放假安排是否为推算值（国务院尚未公布）。未知时按「未公布」返回 false。 */
export function isEstimatedYear(date: string): boolean {
  return getDayInfo(date)?.estimated ?? false
}

/** 已录入官方安排的年份，用于提示用户哪几年的调休是确定的。 */
export function officialYearsLoaded(): number[] {
  return officialYears
}

/**
 * 订阅一组日期的日历信息。
 *
 * 只按「最小日到最大日」发一次请求 —— 日历视图给的是连续区间，
 * 逐日请求会把一个月拆成 42 个接口。
 */
export function useCalendarInfo(dates: string[]): Map<string, CalendarDayInfo> {
  const [, setTick] = useState(0)
  const sorted = [...new Set(dates)].sort()
  const rangeKey = sorted.length ? `${sorted[0]}~${sorted[sorted.length - 1]}` : ''

  useEffect(() => {
    if (!rangeKey) return
    const l = () => setTick((v) => v + 1)
    listeners.add(l)
    void ensure(sorted[0], sorted[sorted.length - 1])
    return () => {
      listeners.delete(l)
    }
    // sorted 每次渲染都是新数组，用 rangeKey 当作依赖才是稳定的。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rangeKey])

  const out = new Map<string, CalendarDayInfo>()
  for (const d of sorted) {
    const info = cache.get(d)
    if (info) out.set(d, info)
  }
  return out
}

/** 日历格里那行小字的配色：节假日用朱砂，节气与普通农历日都是次要灰。 */
export function labelClass(info: CalendarDayInfo | undefined): string {
  if (!info) return 'text-ink-3'
  switch (info.labelKind) {
    case 'festival':
    case 'holiday':
      return 'text-holiday'
    default:
      return 'text-ink-3'
  }
}

/** 拼一句完整的日历描述，用于 title 与读屏标签。 */
export function describeDay(info: CalendarDayInfo | undefined): string {
  if (!info) return ''
  const parts = [info.lunarFull]
  // 节气与「清明节」这类同名项只留一个，避免「清明 · 清明节」这种重复。
  if (info.solarTerm && !info.holiday.startsWith(info.solarTerm)) parts.push(info.solarTerm)
  // 农历节日已经说明了是哪个节，就不再叠一个法定名称（除夕那天是「春节」）。
  if (info.festival) parts.push(info.festival)
  else if (info.holiday) parts.push(info.holiday)
  if (info.kind === 'workday') parts.push('调休上班')
  else if (info.isHoliday) parts.push('放假')
  else if (info.holidaySpan) parts.push(`${info.holidaySpan}假期`)
  return parts.join(' · ')
}

/** 日历格角落要不要挂「休 / 班」标记：只在「本来要上班却放假」时才挂，周末不必提醒。 */
export function workMark(info: CalendarDayInfo | undefined): '休' | '班' | null {
  if (!info) return null
  if (info.kind === 'workday') return '班'
  if (info.kind === 'holiday' || info.kind === 'rest') return '休'
  return null
}
