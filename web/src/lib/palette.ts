/** 清单 / 标签的配色板，与浅深两套主题都能共处。 */

export const PALETTE = [
  '#b4553d',
  '#c98a2e',
  '#6b7f6e',
  '#5c6b8a',
  '#7a5c7a',
  '#3f7a7a',
  '#8a6d3b',
  '#a8455c',
  '#4f7f9e',
  '#8a8f98',
]

export const ACCENTS: { value: string; label: string; color: string }[] = [
  { value: 'seal', label: '朱砂', color: '#b4553d' },
  { value: 'azure', label: '黛青', color: '#4a6386' },
  { value: 'jade', label: '竹青', color: '#4f7358' },
  { value: 'violet', label: '绛紫', color: '#6f4f74' },
  { value: 'ink', label: '松烟', color: '#4a4741' },
]

export function paletteAt(index: number): string {
  return PALETTE[index % PALETTE.length]
}

/**
 * 色板的中文色名。
 * 色块按钮原先直接拿十六进制当 aria-label，读屏会一个字符一个字符地念
 * 「井号 b 四 五 五 三 d」。这里给每个颜色一个名字，念出来才是有意义的。
 */
const COLOR_NAMES: Record<string, string> = {
  '#b4553d': '朱砂',
  '#c98a2e': '琥珀',
  '#6b7f6e': '苔青',
  '#5c6b8a': '黛蓝',
  '#7a5c7a': '紫檀',
  '#3f7a7a': '青碧',
  '#8a6d3b': '赭褐',
  '#a8455c': '胭脂',
  '#4f7f9e': '天青',
  '#8a8f98': '烟灰',
  '#8a7c66': '土褐',
}

/** 取颜色的中文名；未登记的颜色退回原值，避免漏配时读成空白。 */
export function colorName(hex: string): string {
  return COLOR_NAMES[hex.toLowerCase()] ?? hex
}
