/**
 * 「慎始」PWA / 离线能力冒烟测试。
 *
 * UI 冒烟证明「联网时能用」，这个脚本证明另外三件事：
 *   1. 装得下来：manifest、图标、Service Worker 都真的到位（iOS 装 PWA 的硬门槛）。
 *   2. 断网不白屏：还能看到已有的任务，并且能新建、能完成。
 *   3. 联网能补账：离线期间攒下的操作会重放到服务端，且不重不漏。
 *
 * 用法：
 *   scripts/build.sh && node scripts/pwa-smoke.mjs
 *
 * 可选环境变量：
 *   SHENSHI_BIN   指定被测二进制（默认 <repo>/bin/shenshi）
 *   CHROMIUM_PATH 指定 Chromium 可执行文件（默认自动探测）
 *   SHOT_DIR      截图输出目录（默认 /tmp/shenshi-pwa-shots）
 *
 * 断网用 Playwright 的 context.setOffline(true)：它拦的是浏览器的网络栈，
 * Service Worker 的 fetch 同样被拦，与真机拔网线等价；直接掐掉后端进程
 * 只能验证「服务没响应」，验证不了 Service Worker 的缓存回退。
 *
 * 实例用 -web 指向磁盘上的 web/dist，而不是内嵌资源：更新检测那一节要改 sw.js
 * 来触发一次真实的更新，内嵌的副本改不动。两者走的是同一个 spaHandler、同一套
 * HTTP 响应头，内嵌路径由 ui-smoke.mjs 覆盖。
 */

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

import {
  defaultBinary,
  findChromium,
  loadPlaywright,
  repoRoot,
  shutdown,
  spawnInstance,
  waitHealthy,
  watchPageErrors,
} from './lib/harness.mjs'

const { chromium } = loadPlaywright()
const shotDir = process.env.SHOT_DIR || '/tmp/shenshi-pwa-shots'

const passed = []
const failed = []

function check(name, ok, detail = '') {
  if (ok) {
    passed.push(name)
    console.log(`  ✓ ${name}`)
  } else {
    failed.push(`${name}${detail ? ` — ${detail}` : ''}`)
    console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`)
  }
}

function section(title) {
  console.log(`\n${title}`)
}

/** 轮询等待某个条件成立，避免写死延时导致 CI 上偶发失败。 */
async function until(fn, timeoutMs = 8000, step = 200) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await fn()
    if (last) return last
    await new Promise((r) => setTimeout(r, step))
  }
  return last
}

/** ① 可安装性：装 PWA 的硬门槛，逐条查。 */
async function suiteInstallable(page, base) {
  section('① 可安装性')
  await page.goto(base, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('text=收集箱', { timeout: 15000 })

  const manifestHref = await page.getAttribute('link[rel="manifest"]', 'href')
  check('页面声明了 manifest', manifestHref === '/manifest.webmanifest', String(manifestHref))

  const manifest = await (await fetch(`${base}/manifest.webmanifest`)).json()
  check(
    'manifest 可解析且有 name',
    typeof manifest.name === 'string' && manifest.name.length > 0,
    JSON.stringify(manifest.name),
  )
  check('manifest 声明 standalone', manifest.display === 'standalone', String(manifest.display))
  check('manifest 声明 start_url', manifest.start_url === '/', String(manifest.start_url))

  // iOS 装 PWA 的两道硬门槛：apple-touch-icon 必须是 PNG 且不透明。
  const apple = await page.getAttribute('link[rel="apple-touch-icon"]', 'href')
  check('页面声明了 apple-touch-icon', !!apple, String(apple))
  const appleBuf = Buffer.from(await (await fetch(base + apple)).arrayBuffer())
  const isPng = appleBuf
    .subarray(0, 8)
    .equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
  check('apple-touch-icon 是真 PNG', isPng, appleBuf.subarray(0, 4).toString('hex'))
  // PNG 的第 25 字节是 color type：2=RGB，6=RGBA。iOS 不接受带 alpha 的图标。
  check('apple-touch-icon 不透明', appleBuf[25] === 2, `color type ${appleBuf[25]}`)

  const capable = await page.getAttribute('meta[name="apple-mobile-web-app-capable"]', 'content')
  check('声明 apple-mobile-web-app-capable', capable === 'yes', String(capable))

  for (const size of ['192x192', '512x512']) {
    const icon = manifest.icons.find((i) => i.sizes === size)
    const ok = icon && (await fetch(base + icon.src)).ok
    check(`manifest 图标 ${size} 可取到`, !!ok, String(icon?.src))
  }
  const maskable = manifest.icons.some((i) => (i.purpose || '').includes('maskable'))
  check('提供 maskable 图标', maskable, JSON.stringify(manifest.icons.map((i) => i.purpose)))

  // 应用外壳一旦被浏览器的 HTTP 缓存留住，发新版后拿到的还是旧 HTML，
  // 它引用的哈希资源又已经在下一次构建里删掉了 —— 那就是一次白屏。
  const shell = await fetch(`${base}/`, { cache: 'no-store' })
  const shellCC = shell.headers.get('cache-control') || ''
  check('index.html 每次都回源校验', /no-cache|no-store/.test(shellCC), `Cache-Control: ${shellCC || '（未设置）'}`)

  // Go 标准库不认识 .webmanifest，不显式声明就会嗅探成 text/plain，
  // 浏览器据此记一条 MIME 不合规的告警。
  const manifestResp = await fetch(`${base}/manifest.webmanifest`, { cache: 'no-store' })
  check(
    'manifest 不缓存',
    /no-cache|no-store/.test(manifestResp.headers.get('cache-control') || ''),
    `Cache-Control: ${manifestResp.headers.get('cache-control') || '（未设置）'}`,
  )
  check(
    'manifest 以 application/manifest+json 提供',
    (manifestResp.headers.get('content-type') || '').includes('application/manifest+json'),
    `Content-Type: ${manifestResp.headers.get('content-type')}`,
  )

  const swCC = (await fetch(`${base}/sw.js`, { cache: 'no-store' })).headers.get('cache-control') || ''
  check('sw.js 不缓存', /no-cache|no-store/.test(swCC), `Cache-Control: ${swCC || '（未设置）'}`)
}

/**
 * ② Service Worker 真的起来了，并且已经建立缓存。
 *
 * 注意这里**不**检查接口缓存：首次访问时页面还没有 controller，它发出的请求
 * 根本不经过 Service Worker，自然也不会进缓存。要验证接口缓存必须等页面被
 * 接管之后，那一步在 suiteOffline 里 reload 完再做。
 */
async function suiteServiceWorker(page) {
  section('② Service Worker')
  const reg = await until(async () => {
    const r = await page.evaluate(async () => {
      const reg = await navigator.serviceWorker.ready
      return { active: !!reg.active, state: reg.active?.state }
    })
    return r.active ? r : null
  }, 15000)
  check('Service Worker 已激活', !!reg, JSON.stringify(reg))

  const cacheNames = await page.evaluate(() => caches.keys())
  check('已建立缓存', cacheNames.some((n) => n.startsWith('shenshi-shell-')), JSON.stringify(cacheNames))
}

/** ③④ 断网读写，联网补账。 */
async function suiteOffline(context, page, base, today) {
  // 联网时先建一条任务，作为断网后要看到的存量。
  const seeded = await (
    await fetch(`${base}/api/tasks`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: '断网前记下的事', dueDate: today }),
    })
  ).json()
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('text=断网前记下的事', { timeout: 10000 })
  check('联网时任务正常显示', true)

  // 重新加载后 SW 才是 controller，页面才算被它接管。
  const controlled = await until(async () => page.evaluate(() => !!navigator.serviceWorker.controller), 8000)
  check('页面已由 Service Worker 接管', !!controlled, String(controlled))

  // 接管之后页面发出的请求才会经过 SW，离线时看的就是这份缓存。
  // 缓存写入是异步的，轮询等它落盘。
  const apiCached = await until(async () => {
    const names = await page.evaluate(() => caches.keys())
    const apiCache = names.find((n) => n.startsWith('shenshi-api-'))
    if (!apiCache) return false
    return page.evaluate(async (name) => (await (await caches.open(name)).keys()).length, apiCache)
  }, 12000)
  check('已缓存接口响应', !!apiCached, String(apiCached))

  section('③ 断网：查看 / 新建 / 完成')
  await context.setOffline(true)
  await page.waitForTimeout(500)
  check('浏览器报告已离线', (await page.evaluate(() => navigator.onLine)) === false)

  // 重新加载一次：这是最严苛的场景——连应用外壳都得从缓存里起来。
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('text=慎始', { timeout: 15000 })
  await page.waitForSelector('text=断网前记下的事', { timeout: 15000 })
  check('断网后应用仍能启动', true)
  check('断网后仍能看到已有任务', (await page.getByText('断网前记下的事').count()) > 0)

  // Playwright 的 setOffline 走 CDP 的网络条件模拟，只让请求失败，并不把
  // navigator.onLine 翻成 false（真机拔线时会翻）。这里手动派发 offline 事件，
  // 验的是「收到事件后界面如实转为离线」这一段。
  await page.evaluate(() => window.dispatchEvent(new Event('offline')))
  await page.waitForSelector('text=当前离线', { timeout: 5000 })
  check('断网后工具栏显示离线状态', (await page.getByText('当前离线').count()) > 0)
  await page.screenshot({ path: path.join(shotDir, '01-offline-read.png') })

  // 断网新建。任务先上屏、toast 后弹，两处都轮询，别去比谁先出现。
  const quick = page.locator('#shenshi-quickadd')
  await quick.fill('断网时记下的事')
  await quick.press('Enter')
  check(
    '断网后仍可新建任务',
    !!(await until(async () => (await page.getByText('断网时记下的事').count()) > 0, 8000)),
  )
  check(
    '新建后提示已离线保存',
    !!(await until(async () => (await page.getByText(/已离线记下/).count()) > 0, 8000)),
  )
  await page.screenshot({ path: path.join(shotDir, '02-offline-create.png') })

  // 断网完成一条已存在的任务。
  // 先把上一条新建的 toast 等掉：它浮在列表上方，不等就点下一行，
  // 偶发会落到 toast 上——这种 flaky 很难在事后复现，宁可在这里等干净。
  await until(async () => (await page.getByText(/已离线记下/).count()) === 0, 8000)
  const rowSel = 'div.group\\/row'
  await page.locator(rowSel).filter({ hasText: '断网前记下的事' }).first().locator('button[title="标记为已完成"]').first().click()
  const flipped = await until(
    async () =>
      (await page
        .locator(rowSel)
        .filter({ hasText: '断网前记下的事' })
        .first()
        .locator('button[title="标记为未完成"]')
        .count()) > 0,
    8000,
  )
  check('断网后仍可完成任务（界面即翻转）', !!flipped)
  check('顶部显示待同步条数', (await page.getByText(/项待同步/).count()) > 0)
  await page.screenshot({ path: path.join(shotDir, '03-offline-done.png') })

  // 这条完成此时只在本机，服务端绝不能已经知道。
  const mid = await (await fetch(`${base}/api/tasks/${seeded.id}`)).json()
  check('离线完成尚未送达服务端', mid.status === 'todo', `服务端 status=${mid.status}`)

  section('④ 恢复联网：自动补账')
  await context.setOffline(false)
  // 同样手动派发 online：真机插回网线时浏览器会派发，Playwright 的网络条件模拟不会。
  // 应用侧另有 20 秒的兜底重试（见 AppStore），此处走的是主路径。
  await page.evaluate(() => window.dispatchEvent(new Event('online')))

  // 补账是异步的，等服务端真的收到。
  const synced = await until(async () => {
    const r = await (await fetch(`${base}/api/tasks?status=all`)).json()
    const done = (r.tasks || []).find((t) => t.title === '断网前记下的事')
    const created = (r.tasks || []).find((t) => t.title === '断网时记下的事')
    return done?.status === 'done' && !!created ? { done, created } : null
  }, 25000, 400)
  check('离线新建的任务已落到服务端', !!synced, '未在超时内出现')
  check('离线完成已补记到服务端', synced?.done?.status === 'done', synced?.done?.status)

  // 补账后必须对账：界面上那条「断网时记下的事」应换成服务端那条真实记录。
  const settled = await until(async () => (await page.getByText(/项待同步/).count()) === 0, 15000, 400)
  check('待同步条数归零', !!settled)
  check('补账后界面回到正常数据', (await page.getByText('断网时记下的事').count()) > 0)
  check('离线状态条已消失', (await page.getByText('当前离线').count()) === 0)
  await page.screenshot({ path: path.join(shotDir, '04-synced.png') })

  // 队列清空后，临时任务不应还留在本地库里。
  const leftovers = await page.evaluate(async () => {
    const db = await new Promise((res) => {
      const r = indexedDB.open('shenshi-offline')
      r.onsuccess = () => res(r.result)
      r.onerror = () => res(null)
    })
    if (!db) return -1
    return new Promise((res) => {
      const r = db.transaction('localTasks', 'readonly').objectStore('localTasks').getAll()
      r.onsuccess = () => res(r.result.length)
      r.onerror = () => res(-1)
    })
  })
  check('临时任务已从本地库清除', leftovers === 0, String(leftovers))
}

/**
 * ⑤ 更新提示。
 *
 * 单独开一个 context：前面的离线折腾（尤其 setOffline）会污染浏览器对
 * Service Worker 更新检查的节流记账，导致这一段假阴性。真实世界里更新检测
 * 发生在正常联网时，用干净的上下文才测得到真东西。
 */
async function suiteUpdate(browser, base) {
  section('⑤ 更新提示')
  const context = await browser.newContext({ locale: 'zh-CN' })
  const page = await context.newPage()
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('text=收集箱', { timeout: 15000 })
    // 首次访问装的是无 controller 的版本，重载一次才进入「有旧版本接管」的状态。
    const ready = await until(
      async () => page.evaluate(async () => (await navigator.serviceWorker.ready).active?.state === 'activated'),
      15000,
    )
    if (!ready) {
      check('新版本 SW 进入等待状态', false, '初次 Service Worker 未就绪')
      return
    }
    await page.reload({ waitUntil: 'domcontentloaded' })
    const controlled = await until(async () => page.evaluate(() => !!navigator.serviceWorker.controller), 8000)
    if (!controlled) {
      check('新版本 SW 进入等待状态', false, '页面未被 Service Worker 接管')
      return
    }

    // 改一下磁盘上的 sw.js：追加一行注释即算内容变化，
    // Service Worker 的更新判定就是比对脚本字节。
    const swPath = path.join(repoRoot, 'web', 'dist', 'sw.js')
    const swSrc = fs.readFileSync(swPath, 'utf8')
    fs.writeFileSync(swPath, `${swSrc}\n// pwa-smoke: 触发一次更新\n`, 'utf8')
    try {
      const probe = await page.evaluate(async () => {
        const reg = await navigator.serviceWorker.getRegistration()
        await reg.update()
        // 有旧版本接管时，新 SW 装好后停在 waiting，不会自动 activate。
        const t0 = Date.now()
        while (Date.now() - t0 < 20000) {
          const r = await navigator.serviceWorker.getRegistration()
          if (r?.waiting) return { ok: true }
          await new Promise((res) => setTimeout(res, 300))
        }
        return { ok: false, why: `未进入 waiting（installing=${reg.installing?.state ?? '无'}）` }
      })
      check('新版本 SW 进入等待状态', probe.ok === true, probe.why ?? '未检测到更新')

      if (probe.ok) {
        const taken = await page.evaluate(async () => {
          const reg = await navigator.serviceWorker.ready
          if (!reg.waiting) return false
          await new Promise((resolve) => {
            navigator.serviceWorker.addEventListener('controllerchange', resolve, { once: true })
            reg.waiting.postMessage({ type: 'SKIP_WAITING' })
            setTimeout(resolve, 3000)
          })
          return true
        })
        check('可让新版本立刻接管（点「更新」走的就是这条）', taken)

        // 新 SW 接管后 activate 会删掉不在白名单里的旧缓存。
        const names = await page.evaluate(() => caches.keys())
        check(
          '激活后只保留当前构建的缓存',
          names.filter((n) => n.startsWith('shenshi-')).length === 2,
          JSON.stringify(names),
        )
      }
    } finally {
      fs.writeFileSync(swPath, swSrc, 'utf8')
    }
  } finally {
    await context.close()
  }
}

async function main() {
  if (!fs.existsSync(defaultBinary)) {
    throw new Error(`未找到被测二进制 ${defaultBinary}，请先执行 scripts/build.sh`)
  }
  fs.mkdirSync(shotDir, { recursive: true })

  const { proc, base, tmp } = await spawnInstance(defaultBinary, [
    '-web',
    path.join(repoRoot, 'web', 'dist'),
  ])
  let browser
  try {
    await waitHealthy(base)
    // 当日仪式会自动弹窗，挡住交互；先标记为已完成。
    const today = new Date().toLocaleDateString('sv-SE')
    await fetch(`${base}/api/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ morningPlanDone: today, reviewDone: today }),
    })

    browser = await chromium.launch({ executablePath: findChromium(), headless: true })
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })
    const page = await context.newPage()
    const { consoleErrors, pageErrors } = watchPageErrors(page, {
      // 断网阶段请求失败是预期行为，浏览器仍会记一条 Failed to load resource；
      // 只断言「无脚本异常」，网络噪音不算。
      ignorable: /Failed to load resource|net::ERR_INTERNET_DISCONNECTED|net::ERR_FAILED/i,
    })

    await suiteInstallable(page, base)
    await suiteServiceWorker(page)
    await suiteOffline(context, page, base, today)

    section('⑥ 零报错')
    check('无未捕获异常', pageErrors.length === 0, pageErrors.join(' | '))
    check('无控制台错误', consoleErrors.length === 0, consoleErrors.slice(0, 3).join(' | '))
    await context.close()

    await suiteUpdate(browser, base)
  } finally {
    if (browser) await browser.close()
    await shutdown(proc)
    fs.rmSync(tmp, { recursive: true, force: true })
  }

  const total = passed.length + failed.length
  console.log(`\n${'='.repeat(56)}`)
  console.log(`通过 ${passed.length}/${total}`)
  if (failed.length) {
    console.log('\n失败项:')
    for (const f of failed) console.log(`  - ${f}`)
    process.exit(1)
  }
  console.log('\u001b[32m全部通过 ✓\u001b[0m')
}

await main()
