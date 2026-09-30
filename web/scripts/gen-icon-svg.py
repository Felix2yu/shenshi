#!/usr/bin/env python3
"""从 LiBianLiShuTi-2.otf 提取「慎」字形轮廓，生成 PWA 用的 icon.svg（512 画布）。

设计与 gen-icons.mjs 的 icon-512.png 同参数：圆角章面 rx=0.205、
印章内框 inset=0.075 / rx=0.12 / stroke=0.024、字高对齐 PNG 的墨迹高度。
字形转成 <path>，SVG 不依赖任何字体文件，跨机器渲染一致。
"""
import math
from fontTools.ttLib import TTFont
from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.svgPathPen import SVGPathPen

FONT = '/Users/yufei/git/shenshi/web/scripts/fonts/LiBianLiShuTi-2.otf'
OUT = '/Users/yufei/git/shenshi/web/public/icons/icon.svg'
CHAR = '慎'
SEAL = '#b4553d'
CREAM = '#fdf6ef'

SIZE = 512
RX = round(SIZE * 0.205)
INSET = round(SIZE * 0.075)
FRAME_RX = round(SIZE * 0.12)
STROKE = round(SIZE * 0.024)
# 与 icon-512.png 同一换算：font-size = 0.66*SIZE（此字形墨迹 1006x732 upem，
# 扁宽是隶书特征，不能按 0.9em 见方去凑，否则横向顶出内框）。
FONT_PX = round(SIZE * 0.66)

font = TTFont(FONT)
upem = font['head'].unitsPerEm
gname = font.getBestCmap()[ord(CHAR)]
glyph_set = font.getGlyphSet()

bp = BoundsPen(glyph_set)
glyph_set[gname].draw(bp)
x_min, y_min, x_max, y_max = bp.bounds
ink_w, ink_h = x_max - x_min, y_max - y_min
ink_cx, ink_cy = (x_min + x_max) / 2, (y_min + y_max) / 2

pen = SVGPathPen(glyph_set)
glyph_set[gname].draw(pen)
path = pen.getCommands()

s = FONT_PX / upem
cx = cy = SIZE / 2
transform = (
    f'translate({cx} {cy}) scale({s:.6f} {-s:.6f}) translate({-ink_cx} {-ink_cy})'
)

svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="{SIZE}" height="{SIZE}" viewBox="0 0 {SIZE} {SIZE}">
  <!-- 「慎」字形取自 LiBianLiShuTi-2.otf（fontTools 转 outline，upem={upem}，
       墨迹 {ink_w}x{ink_h} units），不依赖系统字体。重生成见本文件头注释。 -->
  <rect width="{SIZE}" height="{SIZE}" rx="{RX}" fill="{SEAL}"/>
  <rect x="{INSET}" y="{INSET}" width="{SIZE - INSET * 2}" height="{SIZE - INSET * 2}" rx="{FRAME_RX}"
        fill="none" stroke="{CREAM}" stroke-width="{STROKE}"/>
  <path d="{path}" fill="{CREAM}" transform="{transform}"/>
</svg>
'''
with open(OUT, 'w') as f:
    f.write(svg)
print(f'{OUT}  ink={ink_w}x{ink_h}upem  scale={s:.4f}  bytes={len(svg)}')
