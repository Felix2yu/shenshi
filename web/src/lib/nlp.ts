/**
 * 自然语言快速添加解析器。
 *
 * 目标是把「明天下午 3 点开会 #工作 !高 @重要 每天」这类一句话，
 * 拆成结构化字段。设计上有两点取舍：
 *
 *  1. 就地剥离：每命中一个片段就把它从原串里抹掉，剩余部分即任务标题，
 *     因此用户无需关心书写顺序。
 *  2. 只做确定性识别，不做猜测：识别不到的文本原样保留在标题里，
 *     宁可少识别，也不篡改用户的输入。
 */

import type { Priority } from '../types'
import { addDays, addMonths, daysInMonth, fromDateStr, todayStr, toDateStr, weekday } from './date'

export interface Chip {
  kind: 'date' | 'time' | 'repeat' | 'priority' | 'tag' | 'list' | 'quadrant'
  label: string
}

export interface ParsedInput {
  title: string
  dueDate: string | null
  dueTime: string | null
  repeatRule: string | null
  priority: Priority | null
  important: boolean | null
  urgent: boolean | null
  tagNames: string[]
  listId: number | null
  chips: Chip[]
}

export interface ParseContext {
  today?: string
  lists?: { id: number; name: string }[]
}

const CN_DIGITS: Record<string, number> = {
  零: 0, 〇: 0, 一: 1, 二: 2, 两: 2, 三: 3, 四: 4, 五: 5,
  六: 6, 七: 7, 八: 8, 九: 9, 十: 10,
}

/** 中文数字转阿拉伯数字，支持 1~99 的常见写法。 */
export function cn2num(s: string): number | null {
  if (/^\d+$/.test(s)) return Number(s)
  if (!s) return null
  if (s === '半') return null
  const chars = [...s]
  if (chars.length === 1) {
    const v = CN_DIGITS[chars[0]]
    return v === undefined ? null : v
  }
  const tenIdx = chars.indexOf('十')
  if (tenIdx === -1) {
    // 逐位拼接，如「二三」
    let v = 0
    for (const c of chars) {
      const d = CN_DIGITS[c]
      if (d === undefined || d === 10) return null
      v = v * 10 + d
    }
    return v
  }
  const head = chars.slice(0, tenIdx).join('')
  const tail = chars.slice(tenIdx + 1).join('')
  const h = head === '' ? 1 : CN_DIGITS[head]
  const t = tail === '' ? 0 : CN_DIGITS[tail]
  if (h === undefined || t === undefined) return null
  return h * 10 + t
}

const WEEKDAY_MAP: Record<string, number> = {
  一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6, 日: 0, 天: 0,
}

function removeSpan(src: string, m: RegExpExecArray): string {
  return `${src.slice(0, m.index)} ${src.slice(m.index + m[0].length)}`
}

function tidyTitle(s: string): string {
  return s
    .replace(/\s+/g, ' ')
    .replace(/\s+([,，、;；:：])/g, '$1')
    .replace(/^[\s,，。.、;；:：\-]+/, '')
    .replace(/[\s,，、;；:：\-]+$/, '')
    .trim()
}

/** 解析一句话。ctx.today 可注入固定「今天」，便于测试。 */
export function parseQuickAdd(input: string, ctx: ParseContext = {}): ParsedInput {
  const base = ctx.today || todayStr()
  let src = ` ${input} `
  const chips: Chip[] = []

  const out: ParsedInput = {
    title: '',
    dueDate: null,
    dueTime: null,
    repeatRule: null,
    priority: null,
    important: null,
    urgent: null,
    tagNames: [],
    listId: null,
    chips,
  }

  // ① 标签：#工作
  for (;;) {
    const m = /#([^\s#@!]{1,24})/u.exec(src)
    if (!m) break
    const name = m[1].trim()
    if (name && !out.tagNames.includes(name)) {
      out.tagNames.push(name)
      chips.push({ kind: 'tag', label: `#${name}` })
    }
    src = removeSpan(src, m)
  }

  // ② 清单：/项目推进（只有确实命中清单名时才剥离，避免吃掉正文里的斜杠）
  const listMatch = /(?:^|\s)\/([^\s/@!,，、]{1,24})/u.exec(src)
  if (listMatch) {
    const name = listMatch[1].trim()
    const hit = ctx.lists?.find((l) => l.name === name)
    if (hit) {
      out.listId = hit.id
      chips.push({ kind: 'list', label: `/${hit.name}` })
      src = removeSpan(src, listMatch)
    }
  }

  // ③ 四象限：@重要 / @紧急
  for (;;) {
    const m = /@(重要|紧急)/u.exec(src)
    if (!m) break
    if (m[1] === '重要') out.important = true
    else out.urgent = true
    chips.push({ kind: 'quadrant', label: `@${m[1]}` })
    src = removeSpan(src, m)
  }

  // ④ 优先级：!!! / !! / ! / !高 / !3
  for (;;) {
    const m = /(?:^|\s)!{1,3}(?![\p{Script=Han}\w])/u.exec(src)
    if (!m) break
    const n = (m[0].match(/!/g) || []).length
    out.priority = (n >= 3 ? 3 : n === 2 ? 2 : 1) as Priority
    chips.push({ kind: 'priority', label: `优先级${['无', '低', '中', '高'][out.priority]}` })
    src = removeSpan(src, m)
  }
  if (out.priority === null) {
    const m = /(?:^|\s)!([高中低123])(?![\p{Script=Han}\w])/u.exec(src)
    if (m) {
      const map: Record<string, Priority> = { '1': 1, 低: 1, '2': 2, 中: 2, '3': 3, 高: 3 }
      out.priority = map[m[1]]
      chips.push({ kind: 'priority', label: `优先级${['无', '低', '中', '高'][out.priority]}` })
      src = removeSpan(src, m)
    }
  }

  // ⑤ 重复规则。顺序即优先级：越具体的写法必须越靠前，
  //    否则「每月15日」会被「每月」抢先吞掉。
  const repeatPatterns: { re: RegExp; build: (m: RegExpExecArray) => string | null; label: string }[] = [
    { re: /艾宾浩斯/u, build: () => 'ebbinghaus:0', label: '艾宾浩斯记忆曲线' },
    { re: /每个?工作日/u, build: () => 'weekdays', label: '每个工作日' },
    { re: /每月(最后一天|末)/u, build: () => 'monthly:last', label: '每月最后一天' },
    {
      re: /每月(\d{1,2}|[一二三四五六七八九十]{1,3})[日号]/u,
      build: (m) => {
        const d = cn2num(m[1])
        return d && d >= 1 && d <= 31 ? `monthly:${d}` : 'monthly'
      },
      label: '每月某日',
    },
    {
      re: /每周([一二三四五六日天](?:[、,和][一二三四五六日天])*)/u,
      build: (m) => {
        const days = m[1]
          .split(/[、,和]/)
          .map((c) => WEEKDAY_MAP[c])
          .filter((v) => v !== undefined) as number[]
        return days.length ? `weekly:${days.join(',')}` : 'weekly'
      },
      label: '每周某几日',
    },
    {
      re: /每(\d+|[一二三四五六七八九十两]+)天/u,
      build: (m) => {
        const n = cn2num(m[1])
        return n && n > 1 ? `every:${n}:day` : 'daily'
      },
      label: '每隔若干天',
    },
    {
      re: /每(\d+|[一二三四五六七八九十两]+)周(?![一二三四五六日天])/u,
      build: (m) => {
        const n = cn2num(m[1])
        return n && n > 1 ? `every:${n}:week` : 'weekly'
      },
      label: '每隔若干周',
    },
    { re: /每(?:个)?天|每日/u, build: () => 'daily', label: '每天' },
    { re: /每(?:个)?周(?![\p{Script=Han}])/u, build: () => 'weekly', label: '每周' },
    { re: /每(?:个)?月/u, build: () => 'monthly', label: '每月' },
    { re: /每(?:个)?年/u, build: () => 'yearly', label: '每年' },
  ]
  for (const p of repeatPatterns) {
    if (out.repeatRule) break
    const m = p.re.exec(src)
    if (m) {
      const rule = p.build(m)
      if (rule) {
        out.repeatRule = rule
        chips.push({ kind: 'repeat', label: p.label })
        src = removeSpan(src, m)
      }
    }
  }

  // ⑥ 日期
  const dateSpan = extractDateSpan(src, base)
  if (dateSpan) {
    out.dueDate = dateSpan.date
    chips.push({ kind: 'date', label: dateSpan.label })
    src = dateSpan.src
  }

  // ⑦ 时间（可能带时段前缀）
  const timeInfo = extractTime(src)
  if (timeInfo) {
    out.dueTime = timeInfo.time
    chips.push({ kind: 'time', label: timeInfo.time })
    src = timeInfo.src
  }

  // 只有时间没有日期时，默认落到今天（若已过则顺延到明天）。
  if (out.dueTime && !out.dueDate) {
    const [hh, mm] = out.dueTime.split(':').map(Number)
    const now = new Date()
    const past = hh < now.getHours() || (hh === now.getHours() && mm <= now.getMinutes())
    out.dueDate = past ? addDays(base, 1) : base
    chips.push({ kind: 'date', label: out.dueDate === base ? '今天' : '明天' })
  }

  out.title = tidyTitle(src)
  return out
}

interface DateSpan {
  src: string
  date: string
  label: string
}

/** 识别日期片段并返回剥离后的字符串。 */
function extractDateSpan(source: string, base: string): DateSpan | null {
  let src = source
  const curWd = (weekday(base) - 1 + 7) % 7 // 以周一为 0

  const relative: { re: RegExp; days?: number; label: string }[] = [
    { re: /(?:^|\s)今天|今日/u, days: 0, label: '今天' },
    { re: /(?:^|\s)(?:明天|明日)/u, days: 1, label: '明天' },
    { re: /(?:^|\s)大后天/u, days: 3, label: '大后天' },
    { re: /(?:^|\s)后天/u, days: 2, label: '后天' },
    { re: /(?:^|\s)(?:昨天|昨日)/u, days: -1, label: '昨天' },
  ]
  for (const r of relative) {
    const m = r.re.exec(src)
    if (m) {
      const d = addDays(base, r.days!)
      return { src: removeSpan(src, m), date: d, label: r.label }
    }
  }

  // 「N 天后 / N 周后 / 一个月后」
  let m = /(\d+|[一二三四五六七八九十两]{1,3})\s*(?:天|日)后/u.exec(src)
  if (m) {
    const n = cn2num(m[1]) ?? 1
    return { src: removeSpan(src, m), date: addDays(base, n), label: `${n} 天后` }
  }
  m = /(\d+|[一二三四五六七八九十两]{1,3})\s*(?:周|星期)后/u.exec(src)
  if (m) {
    const n = cn2num(m[1]) ?? 1
    return { src: removeSpan(src, m), date: addDays(base, n * 7), label: `${n} 周后` }
  }
  if (/一(?:个)?月后/u.test(src)) {
    const mm = /一(?:个)?月后/u.exec(src)!
    return { src: removeSpan(src, mm), date: addMonths(base, 1), label: '一个月后' }
  }

  // 「下个月 5 号」
  m = /下(?:个)?月(\d{1,2}|[一二三四五六七八九十]{1,3})?[日号]?/u.exec(src)
  if (m) {
    const next = addMonths(base, 1)
    const [y, mo] = next.split('-').map(Number)
    const day = m[1] ? Math.min(cn2num(m[1]) ?? 1, daysInMonth(y, mo - 1)) : 1
    const date = `${next.slice(0, 7)}-${String(day).padStart(2, '0')}`
    return { src: removeSpan(src, m), date, label: `下月${day}日` }
  }
  if (/月末|月底|本月末|本月底/u.test(src)) {
    const mm = /(?:本月|这个月)?(?:月末|月底)/u.exec(src)!
    const [y, mo] = base.split('-').map(Number)
    const day = daysInMonth(y, mo - 1)
    return { src: removeSpan(src, mm), date: `${base.slice(0, 7)}-${String(day).padStart(2, '0')}`, label: '本月底' }
  }

  // 「下周三」「本周五」「周三」
  m = /(下|下个|本|这)?\s*(?:周|星期|礼拜)([一二三四五六日天])/u.exec(src)
  if (m) {
    const prefix = m[1] || ''
    // WEEKDAY_MAP 用的是 JS 的「周日=0」，这里统一换算成「周一=0」再算差值。
    const targetIdx = (WEEKDAY_MAP[m[2]] - 1 + 7) % 7
    const delta = prefix.startsWith('下') ? 7 - curWd + targetIdx : (targetIdx - curWd + 7) % 7
    const date = addDays(base, delta)
    return { src: removeSpan(src, m), date, label: `周${m[2]}` }
  }

  // 完整日期：2026年9月30日 / 2026-09-30 / 2026.9.30
  m = /(\d{4})\s*[年\-/.]\s*(\d{1,2})\s*[月\-/.]\s*(\d{1,2})\s*[日号]?/u.exec(src)
  if (m) {
    const y = Number(m[1])
    const mo = Number(m[2])
    const d = Number(m[3])
    if (mo >= 1 && mo <= 12 && d >= 1 && d <= 31) {
      const date = `${y}-${String(mo).padStart(2, '0')}-${String(d).padStart(2, '0')}`
      return { src: removeSpan(src, m), date, label: `${mo}月${d}日` }
    }
  }

  // 月日：9月30日 / 9月30。
  // 刻意不支持裸写「9/30」「9-30」——那和「阅读 3/5 章节」这类正文无法区分，
  // 而静默吃掉用户标题里的内容，比少识别一个日期要糟糕得多。
  m = /(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?/u.exec(src)
  if (m) {
    const mo = Number(m[1])
    const d = Number(m[2])
    if (mo >= 1 && mo <= 12 && d >= 1 && d <= 31) {
      const [cy] = base.split('-').map(Number)
      // 若今年的该日期已过，落到明年 —— 「9月1日」在 9 月 20 日的常见预期。
      const thisYear = `${cy}-${String(mo).padStart(2, '0')}-${String(d).padStart(2, '0')}`
      const year = thisYear < base ? cy + 1 : cy
      return {
        src: removeSpan(src, m),
        date: `${year}-${String(mo).padStart(2, '0')}-${String(d).padStart(2, '0')}`,
        label: `${mo}月${d}日`,
      }
    }
  }

  // 单独的「15号」「15日」：本月内若已过则顺延到下月
  m = /(?:^|\s)(\d{1,2}|[一二三四五六七八九十]{1,3})[日号]/u.exec(src)
  if (m) {
    const d = cn2num(m[1])
    if (d && d >= 1 && d <= 31) {
      const [cy, cm] = base.split('-').map(Number)
      if (d <= daysInMonth(cy, cm - 1)) {
        const same = `${cy}-${String(cm).padStart(2, '0')}-${String(d).padStart(2, '0')}`
        if (same >= base) return { src: removeSpan(src, m), date: same, label: `${d}日` }
      }
      const next = addMonths(base, 1)
      const [ny, nm] = next.split('-').map(Number)
      const nd = Math.min(d, daysInMonth(ny, nm - 1))
      return {
        src: removeSpan(src, m),
        date: `${ny}-${String(nm).padStart(2, '0')}-${String(nd).padStart(2, '0')}`,
        label: `${nd}日`,
      }
    }
  }
  return null
}

interface TimeSpan {
  src: string
  time: string
}

const PERIOD_DEFAULT: Record<string, string> = {
  凌晨: '05:00',
  早上: '08:00',
  早晨: '08:00',
  上午: '09:00',
  中午: '12:00',
  下午: '14:00',
  傍晚: '18:00',
  晚上: '20:00',
  夜里: '21:00',
}

/** 识别时间片段。识别不出具体时刻但出现了时段词时，采用该时段的默认时刻。 */
function extractTime(source: string): TimeSpan | null {
  const src = source
  const period = '(凌晨|早上|早晨|上午|中午|下午|傍晚|晚上|夜里)'

  // 一条正则覆盖「下午三点半 / 晚上 8 点 / 16:30 / 下午3:30 / 9：05」，
  // 分流越多越容易漏，合并后时段换算只需一处。
  const re = new RegExp(
    `${period}?\\s*([一二三四五六七八九十两]{1,3}|\\d{1,2})\\s*[点時时:：]\\s*(半|[一二三四五六七八九十]{1,3}|\\d{1,2})?\\s*分?`,
    'u',
  )
  const m = re.exec(src)
  if (m) {
    const word = m[1] || ''
    const raw = cn2num(m[2])
    if (raw !== null && raw <= 24) {
      let h = raw
      let mm = 0
      if (m[3] === '半') mm = 30
      else if (m[3]) mm = cn2num(m[3]) ?? 0

      const pm = word === '下午' || word === '傍晚' || word === '晚上' || word === '夜里'
      const noon = word === '中午'
      const am = word === '凌晨' || word === '早上' || word === '早晨' || word === '上午'

      // 时段词负责消歧；12 点与 24 点在「凌晨 / 夜里」下要归零。
      if ((pm || noon) && h < 12 && h > 0) h += 12
      if ((word === '凌晨' || word === '夜里' || word === '晚上') && h === 12) h = 0
      if ((am || pm || noon) && h === 24) h = 0
      if (h === 24) h = 0

      if (h >= 0 && h <= 23 && mm >= 0 && mm <= 59) {
        return { src: removeSpan(src, m), time: `${String(h).padStart(2, '0')}:${String(mm).padStart(2, '0')}` }
      }
    }
  }

  // 只有时段词时，取该时段的默认时刻（如「下午」→ 14:00）。
  // 这里不加「后面必须是空格」之类的限制：中文里时段词后面直接跟正文是常态。
  const only = new RegExp(`${period}(?![饭餐茶])`, 'u').exec(src)
  if (only) {
    return { src: removeSpan(src, only), time: PERIOD_DEFAULT[only[1]] }
  }
  return null
}

/** 把重复规则转成中文描述。与后端 repeat.go 的语法保持一致。 */
export function describeRepeat(rule: string | null | undefined): string {
  if (!rule) return '不重复'
  const [head, ...rest] = rule.split(':')
  const arg = rest.join(':')
  switch (head) {
    case 'daily':
      return '每天'
    case 'weekdays':
      return '每个工作日'
    case 'weekly':
      if (!arg) return '每周'
      return `每周${arg.split(',').map((n) => '日一二三四五六'[Number(n)] ?? n).join('、')}`
    case 'monthly':
      if (!arg) return '每月'
      if (arg === 'last') return '每月最后一天'
      return `每月 ${arg} 日`
    case 'yearly':
      if (!arg) return '每年'
      return `每年 ${arg.replace('-', ' 月 ')} 日`
    case 'every': {
      const [n, unit] = arg.split(':')
      const unitLabel: Record<string, string> = { day: '天', week: '周', month: '个月', year: '年' }
      return `每 ${n} ${unitLabel[unit] ?? unit}`
    }
    case 'ebbinghaus': {
      const idx = Number(arg || 0)
      const gaps = [1, 2, 4, 7, 15, 30, 60]
      if (idx === 0) return '艾宾浩斯记忆曲线'
      return `艾宾浩斯（第 ${idx + 1} 轮，间隔 ${gaps[idx] ?? '—'} 天）`
    }
    default:
      return rule
  }
}

/** 供快速添加的提示文案。 */
export const QUICK_ADD_HINTS = [
  { syntax: '明天下午 3 点', desc: '识别日期与时间' },
  { syntax: '#标签', desc: '自动打标签' },
  { syntax: '/清单名', desc: '归入指定清单' },
  { syntax: '!高', desc: '设定优先级（! / !! / !!!）' },
  { syntax: '@重要 @紧急', desc: '标记四象限' },
  { syntax: '每天 / 每周一 / 每月15日', desc: '设定重复' },
]
