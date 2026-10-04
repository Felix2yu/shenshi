/**
 * 查询语法的**前端镜像**：只用于两件事——
 *
 * 1. 搜索框里把识别出的算子显示成标签（`#重要`、`@工作`…），让用户看见「这句话被怎么理解了」；
 * 2. 点标签旁的 × 删掉该算子。
 *
 * 真正的筛选在后端做（见 server/internal/store/query.go）。这里刻意复刻同一套规则而不是
 * 让前端猜：**两边规则一旦分叉，用户看到的提示就会和实际结果对不上**，那比没有提示更糟。
 * 新增算子时两处都要改。
 */

/** 一个词：文本 + 是否被引号包住。 */
export interface QueryToken {
  text: string
  quoted: boolean
}

/**
 * 切词：空格分隔，引号内算一词。引号开头的词会被标记 quoted ——
 * 「"#重要 项目"」是在说这一段是字面量，不该被拆成算子。
 */
export function splitQueryTokens(s: string): QueryToken[] {
  const out: QueryToken[] = []
  let cur = ''
  let quoted = false
  let has = false
  let wasQuoted = false
  const flush = () => {
    if (has) {
      const v = cur.trim()
      if (v) out.push({ text: v, quoted: wasQuoted })
    }
    cur = ''
    has = false
    wasQuoted = false
  }
  for (const r of s) {
    if (r === '"') {
      quoted = !quoted
      if (!has) wasQuoted = true
      has = true
    } else if (!quoted && (r === ' ' || r === '\t' || r === '\n')) {
      flush()
    } else {
      cur += r
      has = true
    }
  }
  flush()
  return out
}

const PRIORITY_KEYWORDS: Record<string, number> = {
  '0': 0, '1': 1, '2': 2, '3': 3,
  p0: 0, p1: 1, p2: 2, p3: 3,
  '无': 0, '低': 1, '中': 2, '高': 3,
  none: 0, low: 1, mid: 2, medium: 2, high: 3,
}

const DUE_KEYWORDS = new Set([
  'today', 'tomorrow', 'yesterday', 'overdue', 'week', 'next7', 'nodate',
])

function isDay(s: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(s)
}

/** due: 的取值是否是合法的一天或区间。 */
function isDueValue(v: string): boolean {
  if (DUE_KEYWORDS.has(v.toLowerCase())) return true
  if (v.includes('..')) {
    const [a, b] = v.split('..', 2)
    return isDay(a) && isDay(b)
  }
  return isDay(v)
}

/** 词是不是一个算子。识别不出的永远返回 false —— 宁可当普通文本，也不能吞掉用户输入。 */
export function isQueryOperator(text: string, quoted: boolean): boolean {
  if (quoted) return false
  if (text.startsWith('#')) return text.length > 1
  if (text.startsWith('@')) return text.length > 1
  if (text.startsWith('-#')) return text.length > 2
  if (text.startsWith('-@')) return text.length > 2
  if (text.startsWith('p:')) return text.slice(2).toLowerCase() in PRIORITY_KEYWORDS
  if (text.startsWith('due:')) return isDueValue(text.slice(4))
  return false
}

/** 算子的可读说明，用在标签的 title 上。 */
export function describeOperator(text: string): string {
  if (text.startsWith('-#')) return `排除标签 ${text.slice(2)}`
  if (text.startsWith('-@')) return `排除清单 ${text.slice(2)}`
  if (text.startsWith('#')) return `标签 ${text.slice(1)}`
  if (text.startsWith('@')) return `清单 ${text.slice(1)}`
  if (text.startsWith('p:')) return `优先级 ${text.slice(2)}`
  if (text.startsWith('due:')) return `到期 ${text.slice(4)}`
  return text
}

/** 从查询串里删掉第 index 个词（用于点标签旁的 ×）。 */
export function removeToken(query: string, index: number): string {
  const toks = splitQueryTokens(query)
  if (index < 0 || index >= toks.length) return query
  return toks
    .filter((_, i) => i !== index)
    .map((t) => (t.quoted ? `"${t.text}"` : t.text))
    .join(' ')
}

/** 搜索框 placeholder：空态时教一句语法，比什么都不说要好。 */
export const SEARCH_HINT = '搜索任务 · #标签 @清单 p:高 due:today'