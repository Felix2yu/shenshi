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
