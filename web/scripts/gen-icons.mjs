#!/usr/bin/env node
/**
 * 生成 PWA / Apple 所需的 PNG 图标。
 *
 * 为什么必须是 PNG 而不是 SVG：
 *   - iOS「添加到主屏幕」只认 apple-touch-icon，且要求不透明的 PNG；
 *   - manifest 的 icons 同样不接受 SVG 作为有效图标；
 *   - 页面上现在那个 data URI 的 SVG favicon 只能当标签页图标用，装不了。
 *
 * 图标已入库，正常构建不需要跑这个脚本；只有改了徽标配色或字形才重跑：
 *   node scripts/gen-icons.mjs
 *
 * 需要 Chromium（复用与 scripts/ui-smoke.mjs 相同的查找口径）：
 *   cd web && npx playwright-core install --with-deps chromium
 */

import { createRequire } from 'node:module'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '..')
const outDir = path.join(webRoot, 'public', 'icons')

const require = createRequire(path.join(webRoot, 'noop.js'))
const { chromium } = require('playwright-core')

/** 与 index.html / App.tsx 里的印章色同源，改这里就等于改全站徽标。 */
const SEAL = '#b4553d'
const CREAM = '#fdf6ef'
/**
 * 字体：LibianLiShuTi（隶变隶书体，用户提供），以 base64 内嵌 @font-face 加载，
 * 不依赖系统装了什么——换机器、CI 上跑结果都一样。
 * 文件放 web/scripts/fonts/ 下；实测坑：系统「看似有」的隶书里 STLibu/华文隶书
 * 并未真正安装，写进 font-family 会静默掉到 serif（看着像细隶书其实是宋体），
 * document.fonts.check 也会被系统回退链骗出 true。验证过的系统字体只有
 * Libian SC（隶变-简）与 Baoli SC（报隶-简），仅作内嵌字体缺失时的兜底。
 */
const FONT_FILE = path.join(here, 'fonts', 'LiBianLiShuTi-2.otf')
const FONT_FAMILY = 'ShenshiLishu'
const FONT_STACK = `${FONT_FAMILY}, Libian SC, Baoli SC, Songti SC, serif`

function fontFaceCss() {
  const b64 = fs.readFileSync(FONT_FILE).toString('base64')
  return `@font-face{font-family:${FONT_FAMILY};src:url(data:font/otf;base64,${b64}) format('opentype');}`
}

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
    '找不到 Chromium。可执行 `cd web && npx playwright-core install --with-deps chromium` 安装，' +
      '或用 CHROMIUM_PATH 指定可执行文件。',
  )
}

/**
 * 单枚图标的 SVG。
 *
 * 两种形态的差别不只是圆角：
 *   - 圆角版：装到桌面 / 启动器时不会被系统再裁一次，形状由我们自己定。
 *     四角必须是真透明——macOS 程序坞直接贴 manifest 图标、不再套遮罩，
 *     白角会被渲染成「白方块里嵌一枚圆章」。
 *   - 满幅版（maskable 与 apple-touch-icon）：把裁切权交给系统。iOS 会按固定的
 *     squircle 遮罩裁切，自己先裁一层就会变成「圆角里再套圆角」。
 *
 * fontFrac 是字号相对画布边长的比例。maskable 受 Android 自适应图标的
 * 内切安全区约束（保证可见的是 61% 边长的圆），字形对角线不能超出它，
 * 取 0.47 顶格；其余形态没有这层约束，字放到 0.66~0.72 撑满章面。
 *
 * frame 是印章式内框线，只在大尺寸上有意义；favicon-32 上 2% 的线宽会糊成一团，
 * maskable 上会被圆形遮罩切掉，都要关掉。
 *
 * 字的位置不在这层定：dominant-baseline 各家实现不一，隶书字形的基线留白
 * 也和宋体不同，靠参数猜必歪。渲染后在页面里量 <text> 的 getBBox、
 * 平移字形墨迹中心到画布中心，见 main() 里的居中修正。
 */
function svg({ size, rounded, fontFrac, frame = false }) {
  const r = rounded ? Math.round(size * 0.205) : 0
  // 底色满幅：任何形状遮罩都只在这一层里取，不会露出透明边。
  const bg = `<rect width="${size}" height="${size}" fill="${rounded ? 'none' : SEAL}"/>`
  const plate = rounded ? `<rect width="${size}" height="${size}" rx="${r}" fill="${SEAL}"/>` : ''
  const inset = Math.round(size * 0.075)
  const border = frame
    ? `<rect x="${inset}" y="${inset}" width="${size - inset * 2}" height="${size - inset * 2}" rx="${Math.round(size * 0.12)}"
         fill="none" stroke="${CREAM}" stroke-width="${Math.max(1, Math.round(size * 0.024))}"/>`
    : ''
  const font = Math.round(size * fontFrac)
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
  ${bg}
  ${plate}
  ${border}
  <text x="${size / 2}" y="${size / 2}" font-size="${font}" text-anchor="middle" dominant-baseline="central"
        fill="${CREAM}" font-family="${FONT_STACK}">慎</text>
</svg>`
}

const TARGETS = [
  // 浏览器 / 桌面启动器用的常规图标：圆角 + 透明四角 + 印章内框。
  { file: 'icon-192.png', size: 192, rounded: true, fontFrac: 0.66, frame: true },
  { file: 'icon-512.png', size: 512, rounded: true, fontFrac: 0.66, frame: true },
  // Android 自适应图标：满幅 + 字形顶进圆形安全区，不加内框（会被遮罩切掉）。
  { file: 'icon-maskable-512.png', size: 512, rounded: false, fontFrac: 0.47, frame: false },
  // iOS 主屏幕图标：满幅不透明（系统自己裁 squircle），180×180 是 Apple 认的尺寸。
  { file: 'apple-touch-icon.png', size: 180, rounded: false, fontFrac: 0.68, frame: true },
  // 浏览器标签页书签：太小，不放内框。
  { file: 'favicon-32.png', size: 32, rounded: true, fontFrac: 0.72, frame: false },
]

async function main() {
  if (!fs.existsSync(FONT_FILE)) {
    throw new Error(`缺字体文件 ${FONT_FILE}，把 LiBianLiShuTi-2.otf 放进去再跑。`)
  }
  const face = fontFaceCss()
  fs.mkdirSync(outDir, { recursive: true })
  const browser = await chromium.launch({ executablePath: findChromium(), headless: true })
  try {
    const page = await browser.newPage()
    for (const t of TARGETS) {
      const markup = svg(t)
      await page.setViewportSize({ width: t.size, height: t.size })
      await page.setContent(
        `<style>html,body{margin:0;padding:0;background:transparent;}${face}</style>${markup}`,
        { waitUntil: 'load' },
      )
      // 等内嵌字体真的可用再量再截，否则 getBBox 量到的是回退字形的尺寸。
      await page.evaluate(async (fam) => {
        await document.fonts.load(`100px ${fam}`, '慎')
        await document.fonts.ready
      }, FONT_FAMILY)
      // 墨迹居中修正：量出 <text> 实际渲染的字形包围盒，把它的中心平移到画布中心。
      // 隶书字形的上下留白不对称，dominant-baseline 又各家实现不一，不修必歪。
      await page.evaluate((n) => {
        const t = document.querySelector('text')
        const b = t.getBBox()
        t.setAttribute('x', String(Number(t.getAttribute('x')) + n / 2 - (b.x + b.width / 2)))
        t.setAttribute('y', String(Number(t.getAttribute('y')) + n / 2 - (b.y + b.height / 2)))
      }, t.size)
      // 圆角版必须 omitBackground：不然四角被合成成不透明白，程序坞里就是一块白底。
      // 满幅版反过来：SVG 本身铺满，不透明输出，杜绝任何 alpha 边。
      const buf = await page.screenshot({ omitBackground: t.rounded, type: 'png' })
      fs.writeFileSync(path.join(outDir, t.file), buf)
      process.stdout.write(`  ${t.file.padEnd(26)} ${t.size}×${t.size}  ${(buf.length / 1024).toFixed(1)} KB\n`)
    }
  } finally {
    await browser.close()
  }
  process.stdout.write(`图标已写入 ${outDir}\n`)
}

await main()
