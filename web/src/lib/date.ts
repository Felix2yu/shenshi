/** 日期工具：全部基于本地时区的 'YYYY-MM-DD' 字符串，避免时区漂移。 */

const WEEK_FULL = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

export function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

export function toDateStr(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

export function fromDateStr(s: string): Date {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(y, (m ?? 1) - 1, d ?? 1)
}

export function todayStr(): string {
  return toDateStr(new Date())
}

export function nowHM(): string {
  const d = new Date()
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

export function addDays(s: string, n: number): string {
  const d = fromDateStr(s)
  d.setDate(d.getDate() + n)
  return toDateStr(d)
}

export function addMonths(s: string, n: number): string {
  const d = fromDateStr(s)
  const day = d.getDate()
  d.setDate(1)
  d.setMonth(d.getMonth() + n)
  const last = daysInMonth(d.getFullYear(), d.getMonth())
  d.setDate(Math.min(day, last))
  return toDateStr(d)
}

export function daysInMonth(year: number, month0: number): number {
  return new Date(year, month0 + 1, 0).getDate()
}

/** a 与 b 相差的天数（a - b），按自然日计算。 */
export function dayDiff(a: string, b: string): number {
  const da = fromDateStr(a)
  const db = fromDateStr(b)
  const ms = Date.UTC(da.getFullYear(), da.getMonth(), da.getDate()) -
    Date.UTC(db.getFullYear(), db.getMonth(), db.getDate())
  return Math.round(ms / 86400000)
}

export function weekday(dateStr: string): number {
  return fromDateStr(dateStr).getDay()
}

export function weekdayName(dateStr: string): string {
  return WEEK_FULL[weekday(dateStr)]
}

/** 人类可读的日期：近七日用相对说法，其余用「M月D日」。 */
export function humanDay(dateStr: string, base = todayStr()): string {
  const diff = dayDiff(dateStr, base)
  switch (diff) {
    case 0:
      return '今天'
    case 1:
      return '明天'
    case 2:
      return '后天'
    case -1:
      return '昨天'
    case -2:
      return '前天'
    default:
      return `${fromDateStr(dateStr).getMonth() + 1}月${fromDateStr(dateStr).getDate()}日`
  }
}

/** 完整日期：2026年9月22日 周二 */
export function fullDate(dateStr: string): string {
  const d = fromDateStr(dateStr)
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 ${WEEK_FULL[d.getDay()]}`
}

/** 紧凑日期：09-22 */
export function shortDate(dateStr: string): string {
  const d = fromDateStr(dateStr)
  return `${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

/** 任务行的日期标签：今天 / 明天 / 今天 15:00 / 昨天 / 9月30日 09:00 */
export function dueLabel(dateStr: string, time?: string | null, base = todayStr()): string {
  const diff = dayDiff(dateStr, base)
  const head = humanDay(dateStr, base)
  const suffix = time ? ` ${time}` : ''
  if (diff >= -2 && diff <= 2) return head + suffix
  return `${head}${suffix}`
}

export function isOverdue(dueDate: string | null, status: string, base = todayStr()): boolean {
  if (!dueDate || status === 'done') return false
  return dayDiff(dueDate, base) < 0
}

export function parseHM(t: string): { h: number; m: number } {
  const [h, m] = t.split(':').map(Number)
  return { h: h || 0, m: m || 0 }
}

export function toMinutes(t: string): number {
  const { h, m } = parseHM(t)
  return h * 60 + m
}

export function minutesToHM(total: number): string {
  const h = Math.floor(total / 60)
  const m = total % 60
  return `${pad2(h)}:${pad2(m)}`
}

/** 月视图矩阵：返回 6 行 × 7 列的日期字符串，首列为周起始日。 */
export function monthMatrix(year: number, month0: number, weekStart: 0 | 1 = 1): string[][] {
  const first = new Date(year, month0, 1)
  const lead = (first.getDay() - weekStart + 7) % 7
  const start = new Date(year, month0, 1 - lead)
  const weeks: string[][] = []
  for (let w = 0; w < 6; w++) {
    const row: string[] = []
    for (let i = 0; i < 7; i++) {
      const d = new Date(start.getFullYear(), start.getMonth(), start.getDate() + w * 7 + i)
      row.push(toDateStr(d))
    }
    weeks.push(row)
  }
  return weeks
}

/** 周视图的 7 天。 */
export function weekDays(anchor: string, weekStart: 0 | 1 = 1): string[] {
  const wd = weekday(anchor)
  const lead = (wd - weekStart + 7) % 7
  const start = addDays(anchor, -lead)
  return Array.from({ length: 7 }, (_, i) => addDays(start, i))
}

export function weekdayHeaders(weekStart: 0 | 1 = 1): string[] {
  const order = weekStart === 1 ? [1, 2, 3, 4, 5, 6, 0] : [0, 1, 2, 3, 4, 5, 6]
  return order.map((i) => WEEK_FULL[i])
}

/** 相对时间描述，用于「最近完成」。 */
export function relativeTime(iso: string | null): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const diff = Date.now() - t
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day} 天前`
  return humanDay(toDateStr(new Date(t)))
}

/** 把日期+时间合成为可比较的本地时间戳；缺时间则视为当天 09:00。 */
export function dueTimestamp(dueDate: string | null, dueTime?: string | null): number | null {
  if (!dueDate) return null
  const d = fromDateStr(dueDate)
  const { h, m } = dueTime ? parseHM(dueTime) : { h: 9, m: 0 }
  d.setHours(h, m, 0, 0)
  return d.getTime()
}

export function greeting(): string {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 11) return '晨安'
  if (h < 14) return '午安'
  if (h < 18) return '下午好'
  return '晚安'
}

export { WEEK_FULL }
