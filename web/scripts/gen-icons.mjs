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
const PAPER = '#faf7f2'

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
 *   - 满幅版（maskable 与 apple-touch-icon）：把裁切权交给系统。iOS 会按固定的
 *     squircle 遮罩裁切，自己先裁一层就会变成「圆角里再套圆角」。
 * maskable 还要额外把字缩进中心 ~60% 的安全区，否则 Android 的圆形遮罩会切掉笔画。
 */
function svg({ size, rounded, scale = 1 }) {
  const r = rounded ? Math.round(size * 0.22) : 0
  const box = Math.round(size * scale)
  const offset = (size - box) / 2
  // 底色满幅：任何形状遮罩都只在这一层里取，不会露出透明边。
  const bg = `<rect width="${size}" height="${size}" fill="${rounded ? 'none' : SEAL}"/>`
  const plate = rounded
    ? `<rect x="${offset}" y="${offset}" width="${box}" height="${box}" rx="${r}" fill="${SEAL}"/>`
    : ''
  const font = Math.round(box * 0.56)
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
  ${bg}
  ${plate}
  <text x="${size / 2}" y="${size / 2}" font-size="${font}" text-anchor="middle" dominant-baseline="central"
        fill="${CREAM}" font-family="Songti SC, STSong, SimSun, serif">慎</text>
</svg>`
}

const TARGETS = [
  // 浏览器 / 桌面启动器用的常规图标，自带圆角。
  { file: 'icon-192.png', size: 192, rounded: true, scale: 1 },
  { file: 'icon-512.png', size: 512, rounded: true, scale: 1 },
  // Android 自适应图标：满幅 + 内容缩进安全区。
  { file: 'icon-maskable-512.png', size: 512, rounded: false, scale: 0.62 },
  // iOS 主屏幕图标：满幅不透明，180×180 是 Apple 认的尺寸。
  { file: 'apple-touch-icon.png', size: 180, rounded: false, scale: 0.72 },
  // 启动画面（iOS 全屏启动时那张底图）与浏览器书签。
  { file: 'favicon-32.png', size: 32, rounded: true, scale: 1 },
]

async function main() {
  fs.mkdirSync(outDir, { recursive: true })
  const browser = await chromium.launch({ executablePath: findChromium(), headless: true })
  try {
    const page = await browser.newPage()
    for (const t of TARGETS) {
      const markup = svg(t)
      await page.setViewportSize({ width: t.size, height: t.size })
      await page.setContent(
        `<style>html,body{margin:0;padding:0;background:${t.rounded ? 'transparent' : PAPER};}</style>${markup}`,
        { waitUntil: 'load' },
      )
      const buf = await page.screenshot({ omitBackground: !t.rounded, type: 'png' })
      fs.writeFileSync(path.join(outDir, t.file), buf)
      process.stdout.write(`  ${t.file.padEnd(26)} ${t.size}×${t.size}  ${(buf.length / 1024).toFixed(1)} KB\n`)
    }
  } finally {
    await browser.close()
  }
  process.stdout.write(`图标已写入 ${outDir}\n`)
}

await main()
