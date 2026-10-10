#!/usr/bin/env node
/**
 * 生成 PWA / Apple 所需的 PNG 图标。
 *
 * 唯一真源是 public/icons/favicon.svg：字形由 gen-icon-svg.py 从 LiBianLiShuTi-2.otf
 * 转成 outline（不依赖系统字体），本脚本只负责「把这一枚矢量按各目标尺寸原生栅格化」。
 * 这么做的理由是 —— 字形放大改的是 favicon.svg，而 Safari「添加到程序坞」用的恰恰也是
 * favicon.svg（manifest 里 sizes:"any" 的 SVG 优先级最高），PNG 若走另一条渲染路径，
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
const refSvg = path.join(outDir, 'favicon.svg')

const require = createRequire(path.join(webRoot, 'noop.js'))
const { chromium } = require('playwright-core')

const SEAL = '#b4553d'
const CREAM = '#fdf6ef'
/** 参考图的画布边长，SVG 里的坐标一律以它为分母写成比例，再换算到目标尺寸。 */
const REF = 512

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

function svgFor({ size, rounded, fontFrac, frame }) {
  const { d, frame: fr } = reference()
  const bg = rounded
    ? `<rect width="${size}" height="${size}" rx="${Math.round(size * 0.205)}" fill="${SEAL}"/>`
    : `<rect width="${size}" height="${size}" fill="${SEAL}"/>`
  // 两种「印面」：any 图标是方牌 + 方内框（圆角收边），maskable 不画框（见下）。
  // 满幅底板一律铺印章红、不透明 —— 遮罩切掉的那圈跟可见区域同色，
  // 换任何系统形状（圆 / 水滴 / 圆角方）出来的都是同一枚红底徽标，不会露白。
  const mark = frame
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
  // Android 自适应图标：满幅不透明（自适应遮罩自己会裁形状），**圆底 + 大字、无框**。
  //
  // 背景（2026-10-01 两轮实测后的定案）：Safari「添加到程序坞」对 purpose 含 maskable 的
  // 条目**无条件抢选**（纯 "maskable" 也抢；WebKit 挑选逻辑在 Safari 私有部分，开源树只有
  // ApplicationManifestParser，无开关），呈现方式是取图标中心 80% 直径圆放大充满图标、垫白
  // squircle —— 即这枚图在 Dock 里的有效画布是 0.80 圆，在 Android 上是 61~67% 圆。
  // 字形外接圆 ≈ 0.6209×fontFrac（墨迹 1006×732 宽扁，bbox 对角/半宽 = 1.234）。
  //
  // 单张 maskable 两端不可能都好（Dock 观感 76% 需 f≈0.60，Android 只容忍 ≤0.48），
  // 用户拍板取「圆章加大字」折中：**去环，fontFrac 0.58**。
  //   - Dock：字宽 1.006×0.58/0.80 ≈ 73%（此前带环版 58%，any 方章 76%）；
  //   - Android：「慎」bbox 四角无墨，f=0.52 时实测最大墨迹半径 0.2643×边长（远小于
  //     bbox 对角 0.3229 的理论值），线性外推 f=0.58 ≈ 0.2949，safe zone（0.3056）内
  //     余 ~5px，典型圆遮罩（0.3335）内余 ~20px —— 生成后必须实测复核 < 0.3056×边长。
  //
  // manifest 里它必须**只**声明 purpose:"maskable"：掺 any 没有意义（Safari 反正会抢），
  // 纯 maskable 至少让 Chrome/Android 明确拿这枚。Dock 想要方章只能在 Add to Dock 弹窗或
  // web app 设置里手动换图标（选 icon-512.png，Safari stretch + 垫白 squircle，方章保形）。
  { file: 'icon-maskable-512.png', size: 512, rounded: false, fontFrac: 0.58 },
  // 与 512 成对的低分档：老版 Android（及部分启动器）只取 192。同一套配方，
  // 保证两张 maskable 在同一个自适应遮罩下构图一致。
  { file: 'icon-maskable-192.png', size: 192, rounded: false, fontFrac: 0.58 },
  // iOS 主屏幕图标：满幅不透明（系统自己裁 squircle），180×180 是 Apple 认的尺寸。
  { file: 'apple-touch-icon.png', size: 180, rounded: false, fontFrac: 0.76, frame: true },
  // 浏览器标签页书签：太小，内框线会糊成一团，关掉。
  { file: 'favicon-32.png', size: 32, rounded: true, fontFrac: 0.80, frame: false },
  // macOS 程序坞专用素材：满幅 1024 方章（圆角、内框、字 0.76，与 any 同构图）。
  // 用途见 scripts/make-dock-icon.py —— Safari「添加到程序坞」生成的 icns 会把内容
  // 先缩进 80% 模板再压字（实测 2026-10-01），Dock 里字小；直接替换 web app bundle 里的
  // ApplicationIcon.icns 才能拿到与 any 一致的观感。这张不进 manifest，只喂脚本。
  { file: 'icon-dock-1024.png', size: 1024, rounded: true, fontFrac: 0.76, frame: true },
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
