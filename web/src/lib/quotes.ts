/**
 * 典籍文案。
 * 「慎始」的每个空状态与仪式节点都配一句原文出处，让界面本身承担理念的表达，
 * 而不是靠一堆口号标语。
 */

export interface Quote {
  text: string
  source: string
}

export const QUOTES = {
  inbox: { text: '君子慎始，差若毫厘，缪以千里。', source: '《礼记·经解》' },
  today: { text: '凡事豫则立，不豫则废。', source: '《礼记·中庸》' },
  next7: { text: '善问者如攻坚木，先其易者，后其节目。', source: '《礼记·学记》' },
  overdue: { text: '慎终如始，则无败事。', source: '《老子·六十四章》' },
  done: { text: '靡不有初，鲜克有终。', source: '《诗经·大雅·荡》' },
  nodate: { text: '物有本末，事有终始，知所先后，则近道矣。', source: '《礼记·大学》' },
  all: { text: '致广大而尽精微。', source: '《礼记·中庸》' },
  calendar: { text: '张而不弛，文武弗能也；弛而不张，文武弗为也。', source: '《礼记·杂记下》' },
  quadrant: { text: '知止而后有定，定而后能静。', source: '《礼记·大学》' },
  stats: { text: '日计不足，岁计有余。', source: '《淮南子·泰族训》' },
  search: { text: '博学之，审问之，慎思之，明辨之，笃行之。', source: '《礼记·中庸》' },
  morning: { text: '凡事豫则立，不豫则废。', source: '《礼记·中庸》' },
  review: { text: '吾日三省吾身。', source: '《论语·学而》' },
  focus: { text: '譬如为山，未成一篑，止，吾止也。', source: '《论语·子罕》' },
  board: { text: '物有本末，事有终始。', source: '《礼记·大学》' },
  emptyList: { text: '始条理者，智之事也。', source: '《孟子·万章下》' },
} as const

export type QuoteKey = keyof typeof QUOTES

/** 善始善终 · 页脚随笔，每日轮换。 */
export const FOOTNOTES: Quote[] = [
  { text: '慎始而敬终，行稳致远。', source: '《礼记》义疏' },
  { text: '靡不有初，鲜克有终。', source: '《诗经·大雅·荡》' },
  { text: '慎终如始，则无败事。', source: '《老子·六十四章》' },
  { text: '锲而不舍，金石可镂。', source: '《荀子·劝学》' },
  { text: '凡事豫则立，不豫则废。', source: '《礼记·中庸》' },
  { text: '知所先后，则近道矣。', source: '《礼记·大学》' },
]

export function footnoteOfTheDay(): Quote {
  const idx = new Date().getDate() % FOOTNOTES.length
  return FOOTNOTES[idx]
}

/** 晨省的问候语，按时段给一句短句。 */
export function morningLine(pending: number, overdue: number, done: number): string {
  if (overdue > 0) return `有 ${overdue} 件事已过原定之日，先安顿它们，再谈其他。`
  if (pending === 0 && done > 0) return `今日已了 ${done} 件，始末相顾，很好。`
  if (pending === 0) return '今日无待办。不妨添一件真正要紧的事。'
  if (pending <= 3) return `今日 ${pending} 件事，皆为要务，宜一次做透。`
  return `今日 ${pending} 件事，先择其三，余者可缓。`
}

/** 日省的结语，按当日完成情况给回应。 */
export function reviewLine(done: number, open: number): string {
  if (done === 0 && open === 0) return '今日无记录。留白也是一种节奏。'
  if (done === 0) return '今日尚未收束一件事。择其一，先做成。'
  if (open === 0) return '今日诸事皆了，善始善终，可谓敬终。'
  if (done >= open) return '今日所成多于所余，节奏尚好。'
  return '今日所成少于所余，明日宜先清要务。'
}
