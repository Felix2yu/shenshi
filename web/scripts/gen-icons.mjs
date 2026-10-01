#!/usr/bin/env node
/**
 * 生成 PWA / Apple 所需的 PNG 图标。
 *
 * 唯一真源是 public/icons/icon.svg：字形由 gen-icon-svg.py 从 LiBianLiShuTi-2.otf
 * 转成 outline（不依赖系统字体），本脚本只负责「把这一枚矢量按各目标尺寸原生栅格化」。
 * 这么做的理由是 —— 字形放大改的是 icon.svg，而 Safari「添加到程序坞」用的恰恰也是
 * icon.svg（manifest 里 sizes:"any" 的 SVG 优先级最高），PNG 若走另一条渲染路径，
 * 就必然和程序坞里那枚对不上。同源自此保证。
 *
 * 为什么必须是 PNG 而不是 SVG：
 *   - iOS「添加到主屏幕」只认 apple-touch-icon，且要求不透明的 PNG；
 *   - manifest 的 icons 同样不接受 SVG 作为有效图标；
 *   - 页面上现在那个 data URI 的 SVG favicon 只能当标签页图标用，装不了。
 *
 * 图标已入库，正常构建不需要跑这个脚本；只有改了徽标配色或字形才重跑：
 *   cd web && pnpm gen:icons
 *
 * 需要 Chromium（按 playwright 缓存目录与常见路径查找，也可用 CHROMIUM_PATH 指定）：
 *   cd web && pnpm exec playwright-core install --with-deps chromium
 */

import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '..')
const outDir = path.join(webRoot, 'public', 'icons')
const refSvg = path.join(outDir, 'icon.svg')

const require = createRequire(path.join(webRoot, 'noop.js'))
const { chromium } = require('playwright-core')

const SEAL = '#b4553d'
const CREAM = '#fdf6ef'
/** 参考图的画布边长，SVG 里的坐标一律以它为分母写成比例，再换算到目标尺寸。 */
const REF = 512

/** 圆印（maskable）的印面圆环：半径 / 线宽，都是边长比例。见 TARGETS 里的说明。 */
const RING_R = 0.287
const RING_SW = 0.0293

function playwrightCacheDirs() {
  const dirs = [process.env.PLAYWRIGHT_BROWSERS_PATH].filter(Boolean)
  dirs.push(path.join(process.env.HOME ?? '', '.cache', 'ms-playwright'))
  dirs.push(path.join(process.env.HOME ?? '', 'Library', 'Caches', 'ms-playwright'))
  return [...new Set(dirs)].filter((d) => d && fs.existsSync(d))
}

function findChromium() {
  if (process.env.CHROMIUM_PATH) return process.env.CHROMIUM_PATH

  for (const cache of playwrightCacheDirs()) {
    const dirs = fs.readdirSync(cache).filter((d) => d.startsWith('chromium-'))
    for (const d of dirs.sort().reverse()) {
      const cand = [
        path.join(cache, d, 'chrome-mac-arm64', 'Google Chrome for Testing.app', 'Contents', 'MacOS', 'Google Chrome for Testing'),
        path.join(cache, d, 'chrome-mac', 'Google Chrome for Testing.app', 'Contents', 'MacOS', 'Google Chrome for Testing'),
        path.join(cache, d, 'chrome-mac-arm64', 'Chromium.app', 'Contents', 'MacOS', 'Chromium'),
        path.join(cache, d, 'chrome-linux', 'chrome'),
        path.join(cache, d, 'chrome-linux64', 'chrome'),
      ].find((p) => fs.existsSync(p))
      if (cand) return cand
    }
  }

  const system = [
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/snap/bin/chromium',
  ].find((p) => fs.existsSync(p))
  if (system) return system

  throw new Error(
    '找不到 Chromium。可执行 `cd web && pnpm exec playwright-core install --with-deps chromium` 安装，' +
      '或用 CHROMIUM_PATH 指定可执行文件。',
  )
}

/**
 * 从 icon.svg 里抽出字形 outline 与内框线的几何参数。
 * 字形是单条 <path>，位置信息全在 transform 上（墨迹中心已对齐画布中心），
 * 换尺寸时只需把 transform 里的 scale 与 translate 按比例重算。
 */
function reference() {
  const svg = fs.readFileSync(refSvg, 'utf8')
  const plate = /<rect[^>]*fill="#b4553d"[^>]*\/>/.exec(svg)
  const frame = /<rect[^>]*stroke="#fdf6ef"[^>]*\/>/.exec(svg)
  const glyph = /<path[^>]*fill="#fdf6ef"[^>]*\/>/.exec(svg)
  if (!plate || !glyph) {
    throw new Error(`${refSvg} 结构变了（印章底板/字形缺一），请先跑 gen-icon-svg.py 重生成。`)
  }
  const d = /d="([^"]+)"/.exec(glyph[0])[1]
  // 内框线的几何参数（参考画布下的像素），比例换算时用。
  if (!frame) return { d, frame: null }
  const fw = Number(/stroke-width="([\d.]+)"/.exec(frame[0])[1])
  const fx = Number(/ x="([\d.]+)"/.exec(frame[0])[1])
  return { d, frame: { x: fx, stroke: fw } }
}

/**
 * 字形 transform。
 *
 * outline 坐标是 1000-upem 空间（path 内的数值即 em 的千分位），所以
 * scale = 字号 / 1000，字号 = 画布边长 x fontFrac。校验锚点：size=512、
 * fontFrac=0.76 时 scale 应恰好是 icon.svg 里的 0.389000。
 * 别再额外除以参考资料里的 scale——那一步会把字形放大 2.5 倍（2026-10-01 踩过）。
 */
function glyphTransform(size, fontFrac) {
  const scale = (size * fontFrac) / 1000
  const c = size / 2
  return `translate(${c} ${c}) scale(${scale.toFixed(6)} -${scale.toFixed(6)}) translate(-500 -328)`
}

function svgFor({ size, rounded, fontFrac, frame, ring }) {
  const { d, frame: fr } = reference()
  const bg = rounded
    ? `<rect width="${size}" height="${size}" rx="${Math.round(size * 0.205)}" fill="${SEAL}"/>`
    : `<rect width="${size}" height="${size}" fill="${SEAL}"/>`
  // 两种「印面」：any 图标是方牌 + 方内框（圆角收边），maskable 是圆形印面。
  // 满幅底板一律铺印章红、不透明 —— 遮罩切掉的那圈跟可见区域同色，
  // 换任何系统形状（圆 / 水滴 / 圆角方）出来的都是同一枚红底徽标，不会露白。
  const mark = ring
    ? `<circle cx="${(size / 2).toFixed(2)}" cy="${(size / 2).toFixed(2)}" r="${(size * RING_R).toFixed(2)}"`
      + ` fill="none" stroke="${CREAM}" stroke-width="${Math.max(1, (size * RING_SW).toFixed(2))}"/>`
    : frame
      ? `<rect x="${(fr.x / REF * size).toFixed(2)}" y="${(fr.x / REF * size).toFixed(2)}"`
        + ` width="${((REF - fr.x * 2) / REF * size).toFixed(2)}" height="${((REF - fr.x * 2) / REF * size).toFixed(2)}"`
        + ` rx="${(61 / REF * size).toFixed(2)}" fill="none" stroke="${CREAM}"`
        + ` stroke-width="${Math.max(1, (fr.stroke / REF * size).toFixed(2))}"/>`
      : ''
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">\n`
    + `  ${bg}\n  ${mark}\n`
    + `  <path d="${d}" fill="${CREAM}" transform="${glyphTransform(size, fontFrac)}"/>\n</svg>`
}

const TARGETS = [
  // 浏览器 / 桌面启动器用的常规图标：圆角 + 透明四角 + 印章内框。
  { file: 'icon-192.png', size: 192, rounded: true, fontFrac: 0.76, frame: true },
  { file: 'icon-512.png', size: 512, rounded: true, fontFrac: 0.76, frame: true },
  // Android 自适应图标：满幅不透明（自适应遮罩自己会裁形状），**圆印构图**。
  //
  // 为什么 maskable 不能照抄上面那枚方印（2026-10-01 改）：
  //   adaptive icon 会把这枚图套上系统形状，默认圆形，可见区是 66/108 ≈ 61.1% 边长，
  //   也就是半径 0.3056 以外的一切都注定看不见。而方印的内框边中点距中心 0.43×边长，
  //   切完只剩四条孤立线段挂在红圆边缘 —— 一枚方印章被圆切了角，比没有框还难看。
  //   所以 maskable 重新构图：整块铺满印章红，中心画一枚**真的是圆形的印面**。
  //
  // 圆印几何（全部按边长比例，512 下验算）：
  //   圆环外沿 = 0.287 + 0.0293/2 = 0.302      < 0.3056（66/108 圆遮罩半径）—— 不切边；
  //   字形外接圆 = 半宽 = 0.503×0.46 = 0.2314（墨迹宽 1.006 em，半宽即外接圆），
  //   距环内沿 0.287-0.0147-0.2314 ≈ 0.04 留白 —— 字不压环。
  //   字宽相对可见圆（0.611 直径）占 0.46/0.611 ≈ 75%，和 any 版在方牌里占 76% 同一观感。
  //
  // manifest 里它必须**只**声明 purpose:"maskable"：一旦掺了 any，macOS Safari「添加到
  // 程序坞」就会把这枚圆印当方印用（2026-10-01 实测：purpose 含 any 时 WebKit 优先取它），
  // 程序坞里就变成一枚小圆印贴白底；写纯 maskable，Safari 会按 AnyOrMaskable 回退到
  // icon.svg / 192 / 512 那几枚方印，Android 拿到的才是这枚圆印。
  { file: 'icon-maskable-512.png', size: 512, rounded: false, fontFrac: 0.46, ring: true },
  // iOS 主屏幕图标：满幅不透明（系统自己裁 squircle），180×180 是 Apple 认的尺寸。
  { file: 'apple-touch-icon.png', size: 180, rounded: false, fontFrac: 0.76, frame: true },
  // 浏览器标签页书签：太小，内框线会糊成一团，关掉。
  { file: 'favicon-32.png', size: 32, rounded: true, fontFrac: 0.80, frame: false },
]

async function main() {
  if (!fs.existsSync(refSvg)) {
    throw new Error(`缺 ${refSvg}，先跑 python3 scripts/gen-icon-svg.py 生成。`)
  }
  // 先验一遍参考图结构，免得跑到一半才炸。
  reference()
  fs.mkdirSync(outDir, { recursive: true })
  const browser = await chromium.launch({ executablePath: findChromium(), headless: true })
  try {
    const page = await browser.newPage()
    for (const t of TARGETS) {
      await page.setViewportSize({ width: t.size, height: t.size })
      await page.setContent(
        `<style>html,body{margin:0;padding:0;background:transparent;}</style>${svgFor(t)}`,
        { waitUntil: 'load' },
      )
      // 圆角版必须 omitBackground：不然四角被合成成不透明白，程序坞里就是一块白底。
      // 满幅版反过来：SVG 本身铺满，不透明输出，杜绝任何 alpha 边。
      const buf = await page.screenshot({ omitBackground: t.rounded, type: 'png' })
      fs.writeFileSync(path.join(outDir, t.file), buf)
      process.stdout.write(`  ${t.file.padEnd(26)} ${t.size}×${t.size}  ${(buf.length / 1024).toFixed(1)} KB\n`)
    }
  } finally {
    await browser.close()
  }
  process.stdout.write(`图标已写入 ${outDir}（均渲染自 ${path.relative(webRoot, refSvg)}）\n`)
}

await main()
