/**
 * 浏览器冒烟脚本的公共设施。
 *
 * 从 ui-smoke.mjs 里拆出来，是因为现在有两个脚本要在真浏览器里跑：
 * 前端 UI 冒烟与 PWA / 离线冒烟。找 Chromium 的口径尤其不能各写一份——
 * 两条查找路径不一致，CI 上就会出现「一个过了另一个挂」的怪事。
 */

import { spawn } from 'node:child_process'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

export const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
export const defaultBinary = process.env.SHENSHI_BIN || path.join(repoRoot, 'bin', 'shenshi')

/** 找出项目内（优先）或全局的 playwright-core。 */
export function loadPlaywright() {
  for (const target of [path.join(repoRoot, 'web', 'node_modules', 'playwright-core'), 'playwright-core']) {
    try {
      return createRequire(import.meta.url)(target)
    } catch {
      /* 换下一个候选 */
    }
  }
  throw new Error('未找到 playwright-core，请先执行：cd web && pnpm install --frozen-lockfile')
}

/** 要一个空闲端口：并行跑测试时写死端口会撞车。 */
export function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer()
    srv.unref()
    srv.on('error', reject)
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address()
      srv.close(() => resolve(port))
    })
  })
}

export async function waitHealthy(base, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`${base}/api/health`)
      if (r.ok) return await r.json()
    } catch {
      /* 还没起来 */
    }
    await new Promise((r) => setTimeout(r, 150))
  }
  throw new Error('服务未在预期时间内就绪')
}

/** Playwright 在各平台存放浏览器的缓存根目录。 */
function playwrightCacheDirs() {
  const dirs = []
  // 显式指定优先（CI 里常用）。
  if (process.env.PLAYWRIGHT_BROWSERS_PATH) dirs.push(process.env.PLAYWRIGHT_BROWSERS_PATH)
  // Linux 走 XDG 约定，macOS 走 Library/Caches，两者都可能有。
  if (process.env.XDG_CACHE_HOME) dirs.push(path.join(process.env.XDG_CACHE_HOME, 'ms-playwright'))
  dirs.push(path.join(os.homedir(), '.cache', 'ms-playwright'))
  dirs.push(path.join(os.homedir(), 'Library', 'Caches', 'ms-playwright'))
  return [...new Set(dirs)].filter((d) => fs.existsSync(d))
}

export function findChromium() {
  if (process.env.CHROMIUM_PATH) return process.env.CHROMIUM_PATH

  for (const cache of playwrightCacheDirs()) {
    const dirs = fs.readdirSync(cache).filter((d) => d.startsWith('chromium-'))
    for (const d of dirs.sort().reverse()) {
      const cand = [
        // macOS（Apple Silicon / Intel）
        path.join(cache, d, 'chrome-mac-arm64', 'Google Chrome for Testing.app', 'Contents', 'MacOS', 'Google Chrome for Testing'),
        path.join(cache, d, 'chrome-mac', 'Google Chrome for Testing.app', 'Contents', 'MacOS', 'Google Chrome for Testing'),
        path.join(cache, d, 'chrome-mac-arm64', 'Chromium.app', 'Contents', 'MacOS', 'Chromium'),
        // Linux（CI 与本地都走这条）
        path.join(cache, d, 'chrome-linux', 'chrome'),
        path.join(cache, d, 'chrome-linux64', 'chrome'),
      ].find((p) => fs.existsSync(p))
      if (cand) return cand
    }
  }

  // 最后兜底：系统包管理器装的 Chromium / Chrome。
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

/** 起一个临时实例，返回 { proc, base, tmp }；调用方负责 shutdown + 清理。 */
export function spawnInstance(binary, extraArgs = []) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'shenshi-browser-'))
  return freePort().then((port) => {
    const base = `http://127.0.0.1:${port}`
    const logFile = fs.openSync(path.join(tmp, 'server.log'), 'w')
    const proc = spawn(binary, ['-addr', `:${port}`, '-db', path.join(tmp, 'shenshi.db'), ...extraArgs], {
      stdio: ['ignore', logFile, logFile],
    })
    return { proc, base, tmp, logFile, port }
  })
}

export function shutdown(proc) {
  return new Promise((resolve) => {
    if (!proc || proc.exitCode !== null) return resolve()
    proc.once('exit', () => resolve())
    proc.kill('SIGTERM')
    setTimeout(() => {
      if (proc.exitCode === null) proc.kill('SIGKILL')
      resolve()
    }, 3000)
  })
}

/** 收集控制台错误与未捕获异常：界面能渲染不代表能用。 */
export function watchPageErrors(page, { ignorable } = {}) {
  const consoleErrors = []
  const pageErrors = []
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    const text = msg.text()
    if (ignorable?.test(text)) return
    consoleErrors.push(text)
  })
  page.on('pageerror', (err) => pageErrors.push(String(err)))
  return { consoleErrors, pageErrors }
}
