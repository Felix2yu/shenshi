/**
 * 「慎始」前端 UI 冒烟测试。
 *
 * 用真实 Chromium 打开构建好的单文件服务，走一遍主流程，并在过程中收集
 * 控制台错误与未捕获异常 —— 界面能渲染不代表能用，这里要求「无报错且可交互」。
 *
 * 用法：
 *   node scripts/ui-smoke.mjs
 *
 * 依赖 playwright-core（web 的开发依赖）。浏览器优先复用 Playwright 缓存里的 Chromium，
 * 不会额外下载；找不到缓存时回退到系统安装的 Chromium / Chrome。
 * 缺浏览器可执行：cd web && npx playwright-core install --with-deps chromium
 *
 * 可选环境变量：
 *   SHENSHI_BIN       指定被测二进制（默认 <repo>/bin/shenshi）
 *   CHROMIUM_PATH     指定 Chromium 可执行文件（默认自动探测）
 *   SHOT_DIR          截图输出目录（默认 /tmp/shenshi-shots）
 */

import { spawn } from 'node:child_process'
import { createRequire } from 'node:module'
import fs from 'node:fs'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'
import process from 'node:process'

const require = createRequire(import.meta.url)
const repoRoot = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
const binary = process.env.SHENSHI_BIN || path.join(repoRoot, 'bin', 'shenshi')
const shotDir = process.env.SHOT_DIR || '/tmp/shenshi-shots'

function loadPlaywright() {
  for (const target of [path.join(repoRoot, 'web', 'node_modules', 'playwright-core'), 'playwright-core']) {
    try {
      return require(target)
    } catch {
      /* 换下一个候选 */
    }
  }
  throw new Error('未找到 playwright-core，请先执行：cd web && npm install')
}

const { chromium } = loadPlaywright()

const passed = []
const failed = []

/** 控制台里可以忽略的噪音。 */
const IGNORABLE = /favicon|Download the React DevTools/i
/**
 * 鉴权场景下 401 是预期行为：探测会话是否有效的请求本来就会失败，
 * 而浏览器会把任何非 2xx 的请求都记成一条「Failed to load resource」控制台错误。
 * 因此这条过滤只用于鉴权流程，不要混进主流程的「无控制台错误」检查。
 */
const EXPECTED_AUTH_NOISE = /401|Failed to load resource/i
/** 鉴权测试用的口令，仅在临时实例里使用。 */
const AUTH_TOKEN = 'ui-smoke-token-7c1f'

function check(name, ok, detail = '') {
  if (ok) {
    passed.push(name)
    console.log(`  \u2713 ${name}`)
  } else {
    failed.push(`${name}${detail ? ` — ${detail}` : ''}`)
    console.log(`  \u2717 ${name}${detail ? ` — ${detail}` : ''}`)
  }
}

function section(title) {
  console.log(`\n${title}`)
}

function freePort() {
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

async function waitHealthy(base, timeoutMs = 15000) {
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

function findChromium() {
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
    '找不到 Chromium。可执行 `cd web && npx playwright-core install --with-deps chromium` 安装，' +
      '或用 CHROMIUM_PATH 指定可执行文件。',
  )
}

async function main() {
  if (!fs.existsSync(binary)) {
    throw new Error(`未找到被测二进制 ${binary}，请先执行 scripts/build.sh`)
  }
  fs.mkdirSync(shotDir, { recursive: true })

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'shenshi-ui-'))
  const port = await freePort()
  const base = `http://127.0.0.1:${port}`

  const logFile = fs.openSync(path.join(tmp, 'server.log'), 'w')
  const server = spawn(binary, ['-addr', `:${port}`, '-db', path.join(tmp, 'shenshi.db')], {
    stdio: ['ignore', logFile, logFile],
  })

  let browser
  try {
    const health = await waitHealthy(base)
    section('① 服务启动')
    check('内嵌前端可访问', health.app === '慎始', JSON.stringify(health))

    // 预先标记今日仪式已完成：自动弹出的晨省/日省会遮挡交互，测试里改为显式触发。
    const today = new Date().toLocaleDateString('sv-SE')
    await fetch(`${base}/api/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ morningPlanDone: today, reviewDone: today }),
    })

    browser = await chromium.launch({ executablePath: findChromium(), headless: true })
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })

    const consoleErrors = []
    const pageErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })
    page.on('pageerror', (err) => pageErrors.push(String(err)))

    await page.goto(base, { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('text=收集箱', { timeout: 15000 })
    await page.waitForTimeout(1200) // 等自动仪式判断落地（已预先标记完成，不应弹出）
    check('已完成的当日仪式不再自动弹出', (await page.locator('[role="dialog"]').count()) === 0)

    section('② 首屏结构')
    check('侧边栏智能清单齐全', (await page.getByText('最近 7 天').count()) > 0)
    check('分组「工作」已呈现', (await page.getByText('工作', { exact: true }).count()) > 0)
    check('工具栏标题为「今天」', (await page.locator('h1').first().innerText()).includes('今天'))
    check('空状态给出典籍文案', (await page.getByText(/凡事豫则立|今日事/).count()) > 0)
    await page.screenshot({ path: path.join(shotDir, '01-today.png') })

    // 晨省：手动触发，验证仪式本身可用
    await page.getByText('晨省 · 规划今日').first().click()
    await page.waitForTimeout(800)
    const morningText = await page.locator('[role="dialog"]').first().innerText()
    check('晨省面板可打开且含规划要点', /今日|重点/.test(morningText))
    await page.screenshot({ path: path.join(shotDir, '01b-morning.png') })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)
    check('Esc 关闭晨省', (await page.locator('[role="dialog"]').count()) === 0)

    section('③ 自然语言快速添加')
    const quick = page.locator('#shenshi-quickadd')
    await quick.fill('今天下午4点 复看项目材料 #工作 !高')
    await page.waitForTimeout(150)
    const chips = await page.locator('text=今天').count()
    check('识别出「今天」预览', chips > 0)
    await quick.press('Enter')
    await page.waitForTimeout(800)
    check('任务已出现在今天', (await page.getByText('复看项目材料').count()) > 0)
    check('标签已挂上', (await page.getByText('#工作').count()) > 0)
    await page.screenshot({ path: path.join(shotDir, '02-quickadd.png') })

    section('④ 交互：详情 / 完成 / 搜索')
    // TaskRow 根节点带 group/row 类名，用它定位整行最稳
    const row = page.locator('div.group\\/row').filter({ hasText: '复看项目材料' }).first()

    await row.click({ position: { x: 140, y: 14 } })
    await page.waitForTimeout(500)
    const detailText = await page.locator('aside').last().innerText()
    check('详情面板打开', /子任务|备注|专注/.test(detailText))
    await page.screenshot({ path: path.join(shotDir, '03-detail.png') })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)
    check('Esc 关闭详情面板', (await page.locator('aside').count()) <= 1)

    await row.locator('button[title="标记为已完成"]').first().click()
    await page.waitForTimeout(800)
    const flipped = (await row.locator('button[title="标记为未完成"]').count()) > 0
    check('勾选后切换为完成态', flipped)

    await page.locator('#shenshi-search').fill('复看')
    await page.waitForTimeout(800)
    check('搜索命中目标任务', (await page.getByText('复看项目材料').count()) > 0)
    check('搜索标题反映关键词', (await page.locator('h1').first().innerText()).includes('搜索'))
    await page.locator('#shenshi-search').fill('')
    await page.waitForTimeout(700)
    check(
      '清空搜索后回到原清单（不滞留搜索态）',
      (await page.locator('h1').first().innerText()).includes('今天'),
      await page.locator('h1').first().innerText(),
    )

    section('⑤ 各视图渲染')
    const views = [
      { title: '看板', expect: '拖动卡片可跨列调整' },
      { title: '日历', expect: '回到今天' },
      { title: '四象限', expect: '重要且紧急' },
      { title: '统计', expect: '未完成' },
    ]
    for (const v of views) {
      await page.locator(`button[title="${v.title}"]`).first().click()
      await page.waitForTimeout(700)
      const ok = (await page.getByText(v.expect).count()) > 0
      check(`${v.title}视图渲染`, ok, ok ? '' : `未见「${v.expect}」`)
      await page.screenshot({ path: path.join(shotDir, `04-view-${v.title}.png`) })
    }

    section('⑥ 快捷键与主题')
    await page.locator('button[title="列表"]').first().click()
    await page.waitForTimeout(400)
    await page.keyboard.press('b')
    await page.waitForTimeout(600)
    check('快捷键 B 切到看板', (await page.getByText('拖动卡片可跨列调整').count()) > 0)
    await page.keyboard.press('t')
    await page.waitForTimeout(600)
    check('快捷键 T 回到今天', (await page.locator('h1').first().innerText()).includes('今天'))

    const themeNow = await page.evaluate(() => document.documentElement.dataset.theme)
    check('主题令牌已挂到根节点', themeNow === 'light' || themeNow === 'dark', String(themeNow))

    section('⑦ 手动排序与持久化')
    // 用「无日期」清单：其中任务同属一个分区，拖拽排序的落点判定最确定
    const nav = page.locator('aside').first()
    await nav
      .locator('button')
      .filter({ hasText: '无日期' })
      .first()
      .click()
    await page.waitForTimeout(700)

    const sortQuick = page.locator('#shenshi-quickadd')
    for (const name of ['甲项演练', '乙项演练', '丙项演练']) {
      await sortQuick.fill(name)
      await sortQuick.press('Enter')
      await page.waitForTimeout(500)
    }

    await page.locator('button[title="排序方式"]').first().click()
    await page.waitForTimeout(350)
    await page.getByText('手动', { exact: true }).first().click()
    await page.waitForTimeout(900)

    const grips = page.locator('span[title="拖动调整顺序"]')
    const gripCount = await grips.count()
    check('手动排序模式出现拖拽手柄', gripCount >= 3, `手柄数=${gripCount}`)

    // 手柄的父元素就是任务行，取首行文本作为顺序标识
    const titleAt = async (i) => {
      const text = await grips.nth(i).locator('xpath=..').innerText()
      return text.split('\n').map((s) => s.trim()).filter(Boolean)[0] ?? ''
    }
    const before = [await titleAt(0), await titleAt(1), await titleAt(2)]
    await grips.nth(0).dragTo(grips.nth(2))
    await page.waitForTimeout(1000)
    const after = [await titleAt(0), await titleAt(1), await titleAt(2)]
    const sameSet = [...after].sort().join('|') === [...before].sort().join('|')
    check(
      '拖动后顺序改变且条目不变',
      after[0] !== before[0] && sameSet,
      `${before.join(' / ')} → ${after.join(' / ')}`,
    )
    await page.screenshot({ path: path.join(shotDir, '05-manual-sort.png') })

    // 刷新后重新进入同一视图。这里的判据必须是「仍不同于初始顺序」——
    // 若只比对刷新前后是否一致，一旦乐观更新被回滚也会误判为通过。
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.waitForSelector('text=收集箱', { timeout: 15000 })
    await page.waitForTimeout(900)
    const morningLater2 = page.getByRole('button', { name: '稍后' })
    if (await morningLater2.count()) {
      await morningLater2.first().click()
      await page.waitForTimeout(200)
    }
    await nav
      .locator('button')
      .filter({ hasText: '无日期' })
      .first()
      .click()
    await page.waitForTimeout(1100)
    const reloaded = [await titleAt(0), await titleAt(1), await titleAt(2)]
    check('重新加载后顺序已落库', reloaded.join('|') === after.join('|'), `${after.join(' / ')} → ${reloaded.join(' / ')}`)
    check('落库顺序确实不同于初始', reloaded[0] !== before[0], `${before.join(' / ')} → ${reloaded.join(' / ')}`)

    section('⑧ 数据导出入口')
    await page.locator('button[title="外观与设置"]').first().click()
    await page.waitForTimeout(500)
    // 用 hasText / data-* 而非 getByRole(name)：后者对「中文 + ASCII 混排」的可访问名匹配不可靠
    const exportZip = page.locator('button[data-export-zip]')
    const exportJson = page.locator('button', { hasText: '仅 JSON' })
    const exportCsv = page.locator('button', { hasText: '导出任务表 CSV' })
    check('设置弹窗含完整备份导出', (await exportZip.count()) > 0)
    check('设置弹窗含 JSON 导出', (await exportJson.count()) > 0)
    check('设置弹窗含 CSV 导出', (await exportCsv.count()) > 0)
    check('设置弹窗含导入入口', (await page.locator('button', { hasText: '导入（追加）' }).count()) > 0)
    check(
      '导入框同时接受 zip 与 json',
      ((await page.locator('input[data-import-input]').getAttribute('accept')) || '').includes('.zip'),
      (await page.locator('input[data-import-input]').getAttribute('accept')) || '',
    )

    // 真的点一次导出，确认浏览器收到了附件而非错误页
    const [download] = await Promise.all([
      page.waitForEvent('download', { timeout: 10000 }),
      exportZip.first().click(),
    ])
    const fname = download.suggestedFilename()
    check('导出触发 ZIP 附件下载', /^shenshi-backup-\d{8}-\d{6}\.zip$/.test(fname), fname)
    const savedPath = await download.path()
    const savedSize = savedPath ? fs.statSync(savedPath).size : 0
    check('备份文件内容非空', savedSize > 200, `${savedSize} 字节`)
    // zip 的魔数：确认拿到的是压缩包，而不是错误页被当成文件存下来
    const magic = savedPath ? fs.readFileSync(savedPath).subarray(0, 2).toString('latin1') : ''
    check('下载到的确实是 zip', magic === 'PK', magic)

    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)

    section('⑨ 重复任务：跳过本次')
    await page.keyboard.press('t')
    await page.waitForTimeout(600)
    const repQuick = page.locator('#shenshi-quickadd')
    // 「每天」只设重复规则不带日期，必须补一个「今天」，任务才会落在这个视图里
    await repQuick.fill('每天 今天 静坐一刻钟')
    await repQuick.press('Enter')
    await page.waitForTimeout(900)
    check(
      '重复任务已出现在今天',
      (await page.locator('[data-task-row]').filter({ hasText: '静坐一刻钟' }).count()) > 0,
    )

    // 任务行的根元素带 group/row 标记，用它定位比层层 .last() 稳
    const repRow = page.locator('[data-task-row]').filter({ hasText: '静坐一刻钟' }).first()
    await repRow.hover()
    await page.waitForTimeout(300)
    await repRow.locator('button[title="更多"]').first().click()
    await page.waitForTimeout(400)
    const skipBtn = page.locator('button', { hasText: '跳过本次' })
    check('重复任务菜单出现「跳过本次」', (await skipBtn.count()) > 0)
    await page.screenshot({ path: path.join(shotDir, '06-skip-repeat.png') })

    await skipBtn.first().click()
    await page.waitForTimeout(1100)
    // 跳过会把日期推到明天，因此应当从「今天」视图移出，且不能变成已完成
    check(
      '跳过本次后任务移出今天',
      (await page.locator('[data-task-row]').filter({ hasText: '静坐一刻钟' }).count()) === 0,
    )

    section('⑩ 日历日视图：时间轴排布')
    await page.locator('button[title="日历"]').first().click()
    await page.waitForTimeout(800)
    await page.locator('button[data-cal-mode="day"]').first().click()
    await page.waitForTimeout(900)

    const axis = page.locator('[data-day-axis]')
    check('日视图出现时间轴', (await axis.count()) > 0)
    check('时间轴带整点刻度', (await page.getByText('07:00', { exact: true }).count()) > 0)
    check('日视图出现「全天」区', (await page.locator('[data-day-allday]').count()) > 0)

    // 9:00 对应的纵坐标 = 9h × HOUR_H(52px)。点空白即落一件带时刻的事。
    const Y_9AM = 9 * 52
    await axis.first().click({ position: { x: 120, y: Y_9AM } })
    await page.waitForTimeout(500)
    const adder = page.locator('[data-day-add]')
    check('点时间轴空白处弹出就地点建', (await adder.count()) === 1)
    check('落点吸附到整点 09:00', (await adder.first().getAttribute('data-day-add')) === '09:00')

    await adder.first().locator('input').fill('卯时校书')
    await adder.first().locator('input').press('Enter')
    await page.waitForTimeout(1100)
    const block = page.locator('[data-day-block]').filter({ hasText: '卯时校书' })
    check('带时刻的任务排上了时间轴', (await block.count()) === 1)
    const blockTop = await block.first().evaluate((el) => Math.round(el.getBoundingClientRect().top - el.parentElement.getBoundingClientRect().top))
    check('任务块落在 09:00 的纵位', Math.abs(blockTop - Y_9AM) <= 4, `实际 top=${blockTop}px，期望 ≈${Y_9AM}px`)
    check('任务块标出起止时刻', /\d{2}:\d{2} – \d{2}:\d{2}/.test(await block.first().innerText()))
    await page.screenshot({ path: path.join(shotDir, '07-day-timeline.png') })

    // 拖动任务块到 11:00，只应改时间不动日期
    await block.first().dragTo(axis.first(), { targetPosition: { x: 120, y: 11 * 52 } })
    await page.waitForTimeout(1200)
    const movedTop = await page
      .locator('[data-day-block]')
      .filter({ hasText: '卯时校书' })
      .first()
      .evaluate((el) => Math.round(el.getBoundingClientRect().top - el.parentElement.getBoundingClientRect().top))
    check('拖到 11:00 后纵位随之改变', Math.abs(movedTop - 11 * 52) <= 4, `实际 top=${movedTop}px，期望 ≈${11 * 52}px`)

    // 拖回「全天」区应撤销时刻
    await page
      .locator('[data-day-block]')
      .filter({ hasText: '卯时校书' })
      .first()
      .dragTo(page.locator('[data-day-allday]'))
    await page.waitForTimeout(1200)
    check(
      '拖回全天区后不再占据时间轴',
      (await page.locator('[data-day-block]').filter({ hasText: '卯时校书' }).count()) === 0,
    )
    const allDayChip = page.locator('[data-day-allday]').locator('button', { hasText: '卯时校书' })
    check('任务改为全天事项', (await allDayChip.count()) === 1)
    await page.screenshot({ path: path.join(shotDir, '08-day-allday.png') })

    section('⑪ 习惯打卡')
    await page.keyboard.press('h')
    await page.waitForTimeout(900)
    check('快捷键 H 切到习惯打卡', (await page.locator('h1').first().innerText()).includes('习惯打卡'))
    check('空状态引导先立一个习惯', (await page.getByText(/习惯是日日不断的功夫|立第一个习惯/).count()) > 0)
    await page.screenshot({ path: path.join(shotDir, '09-habits-empty.png') })

    // 新建一个每天一次的习惯
    await page.locator('button', { hasText: '立第一个习惯' }).first().click()
    await page.waitForTimeout(500)
    const habitDialog = page.locator('[role="dialog"]').first()
    await habitDialog.locator('input').first().fill('晨起临帖')
    await page.locator('button', { hasText: '立下' }).first().click()
    await page.waitForTimeout(1100)

    const rows = page.locator('[data-habit-row]')
    check('新建后出现习惯行', (await rows.count()) === 1, String(await rows.count()))
    check('今日尚未打卡', (await rows.first().getAttribute('data-habit-done')) === '0')
    check('工具栏给出今日统计', (await page.getByText(/今日已打卡/).count()) > 0)
    check('出现热力图格子', (await page.locator('[data-habit-cell]').count()) >= 84)

    // 打卡：勾上后行进入完成态，并连成 1 天
    await rows.first().locator('button[title="今日打卡"]').first().click()
    await page.waitForTimeout(1100)
    const doneRow = page.locator('[data-habit-row]').first()
    check('打卡后行进入完成态', (await doneRow.getAttribute('data-habit-done')) === '1')
    check('连续天数显示为 1 天', (await doneRow.getByText('1 天').count()) > 0)
    await page.screenshot({ path: path.join(shotDir, '10-habits-checked.png') })

    // 点昨天的热力格补记，连续天数应当变成 2
    const yDay = new Date(Date.now() - 86400000).toLocaleDateString('sv-SE')
    const habitId = await doneRow.getAttribute('data-habit-row')
    const backfill = page.locator(`[data-habit-cell="${habitId}|${yDay}"]`)
    check('热力图含昨天的格子', (await backfill.count()) === 1)
    await backfill.click()
    await page.waitForTimeout(1100)
    check('补记昨天后连续天数为 2 天', (await page.locator('[data-habit-row]').first().getByText('2 天').count()) > 0)
    check('补记的格子标记为已达成', (await backfill.getAttribute('data-habit-cell-state')) === 'done')

    // 再点一次即撤销补记
    await backfill.click()
    await page.waitForTimeout(1100)
    check('再点一次撤销补记', (await backfill.getAttribute('data-habit-cell-state')) !== 'done')

    // 目标为多次的习惯：+1 累加到达标
    await page.locator('button', { hasText: '新建习惯' }).first().click()
    await page.waitForTimeout(500)
    const d2 = page.locator('[role="dialog"]').first()
    await d2.locator('input').first().fill('日饮八杯水')
    const targetInput = d2.locator('input[type="number"]').first()
    await targetInput.fill('3')
    await page.locator('button', { hasText: '立下' }).first().click()
    await page.waitForTimeout(1100)
    check('共两个习惯', (await rows.count()) === 2, String(await rows.count()))

    const water = page.locator('[data-habit-row]').filter({ hasText: '日饮八杯水' }).first()
    check('多次习惯显示进度', (await water.locator('[data-habit-progress]').innerText()).includes('/3'))
    await water.locator('button[title="再记一次"]').first().click()
    await page.waitForTimeout(900)
    await water.locator('button[title="再记一次"]').first().click()
    await page.waitForTimeout(900)
    check('两次记录后为 2/3', (await water.locator('[data-habit-progress]').innerText()).startsWith('2/3'))
    check('未达标不算今日完成', (await water.getAttribute('data-habit-done')) === '0')
    await water.locator('button[title="再记一次"]').first().click()
    await page.waitForTimeout(1100)
    const waterDone = page.locator('[data-habit-row]').filter({ hasText: '日饮八杯水' }).first()
    check('第三次达到目标即完成', (await waterDone.getAttribute('data-habit-done')) === '1')
    check('今日已打卡计数为 2', (await page.getByText(/今日已打卡/).first().innerText()).includes('2'))
    await page.screenshot({ path: path.join(shotDir, '11-habits-heatmap.png') })

    // 刷新后习惯与打卡仍在
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.waitForSelector('text=收集箱', { timeout: 15000 })
    await page.waitForTimeout(900)
    const morningLater3 = page.getByRole('button', { name: '稍后' })
    if (await morningLater3.count()) {
      await morningLater3.first().click()
      await page.waitForTimeout(200)
    }
    await page.keyboard.press('h')
    await page.waitForTimeout(1100)
    check('刷新后习惯仍在', (await page.locator('[data-habit-row]').count()) === 2)
    check('刷新后今日打卡仍为 2', (await page.getByText(/今日已打卡/).first().innerText()).includes('2'))

    // 删除习惯需二次确认
    page.once('dialog', (d) => void d.accept())
    const toDelete = page.locator('[data-habit-row]').filter({ hasText: '日饮八杯水' }).first()
    await toDelete.hover()
    await page.waitForTimeout(250)
    await toDelete.locator('button[title="更多"]').first().click()
    await page.waitForTimeout(350)
    await page.locator('[role="dialog"]').count()
    await page.locator('button', { hasText: '删除' }).last().click()
    await page.waitForTimeout(1200)
    check('删除后只剩一个习惯', (await page.locator('[data-habit-row]').count()) === 1)

    section('⑫ 备注 Markdown、附件与集成面板')
    // 注意：右下角可能挂着「开启桌面通知」的提示条，但绝不能手动把它从 DOM 里删掉
    // ——那是 React 管理的节点，删了会让整棵树在下次 reconcile 时崩成白屏。
    // 被它挡住的点击一律用 force，让事件直接落在目标元素上。
    // 回到今天视图（上一节停在习惯打卡），并新建一条专用任务，免得依赖别处的残留数据
    await page.locator('button[title="列表"]').first().click({ force: true })
    await page.waitForTimeout(300)
    await page.keyboard.press('t')
    await page.waitForTimeout(700)
    const headingNow = await page.locator('h1').first().innerText()
    check('切回今天视图', headingNow.includes('今天'), headingNow)

    const quick2 = page.locator('#shenshi-quickadd')
    // 带「今天」才会落在今天视图里，否则会进收集箱
    await quick2.fill('今天 Markdown 冒烟任务')
    await quick2.press('Enter')
    await page.waitForTimeout(900)

    await page.waitForTimeout(300)
    const mdRow = page.locator('div.group\\/row').filter({ hasText: 'Markdown 冒烟任务' }).first()
    await mdRow.scrollIntoViewIfNeeded()
    await mdRow.click({ position: { x: 140, y: 14 }, force: true })
    await page.waitForTimeout(900)
    if ((await page.locator('[data-notes-input]').count()) === 0) {
      await page.screenshot({ path: path.join(shotDir, 'debug-detail.png') })
    }
    check('详情面板已打开', (await page.locator('[data-notes-input]').count()) === 1)

    const notesInput = page.locator('[data-notes-input]')
    await notesInput.fill('# 议程\n- 第一项\n**重点**：收束结论')
    await page.waitForTimeout(900) // 备注是去抖保存的，等它落库
    await page.locator('[data-notes-mode="preview"]').click()
    await page.waitForTimeout(300)
    const previewHtml = await page.locator('[data-notes-preview]').innerHTML()
    check('预览把标题渲染成 h1', previewHtml.includes('<h1'), previewHtml.slice(0, 80))
    check('预览把 **重点** 渲染成 strong', previewHtml.includes('<strong>') && !previewHtml.includes('**重点**'), previewHtml.slice(0, 160))
    check('预览保留了列表项', previewHtml.includes('第一项'), previewHtml.slice(0, 160))
    check('预览不会放行原始 HTML', !previewHtml.includes('<script'), previewHtml.slice(0, 80))

    // 切回编辑态，确认内容没被渲染过程改写
    await page.locator('[data-notes-mode="edit"]').click()
    await page.waitForTimeout(250)
    check('切回编辑态仍是原始 Markdown', (await notesInput.inputValue()).includes('**重点**'))

    // 附件：直接给隐藏的 file input 塞文件，绕开系统文件选择框
    const tmpFile = path.join(tmp, 'smoke-attachment.txt')
    fs.writeFileSync(tmpFile, '这是冒烟测试写入的附件内容。')
    await page.locator('[data-attachment-input]').setInputFiles(tmpFile)
    await page.waitForTimeout(1200)
    check('上传后出现附件行', (await page.locator('[data-attachment-row]').count()) >= 1)
    const attachText = await page.locator('[data-attachment-row]').first().innerText()
    check('附件显示文件名', attachText.includes('smoke-attachment.txt'), attachText)
    await page.screenshot({ path: path.join(shotDir, '05-detail-markdown.png') })

    // 存为模板
    await page.locator('[data-save-template]').click({ force: true })
    await page.waitForTimeout(900)
    check('存为模板给出反馈', (await page.getByText('已存为模板').count()) > 0)

    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)

    // 集成与自动化面板
    await page.locator('button[title="外观与设置"]').first().click({ force: true })
    await page.waitForTimeout(400)
    await page.locator('[data-open-integrations]').click({ force: true })
    await page.waitForTimeout(500)
    check('集成面板打开并停在有模板的那一页', (await page.locator('[data-panel="templates"]').count()) === 1)
    check('模板列表里有刚存的模板', (await page.locator('[data-template-row]').count()) >= 1)

    for (const [tab, marker] of [
      ['webhooks', '任务变更时向外部地址推送'],
      ['backup', '每天在设定的时点导出'],
      ['caldav', 'shenshi-tasks'],
    ]) {
      await page.locator(`[data-integration-tab="${tab}"]`).click()
      await page.waitForTimeout(350)
      const seen = (await page.getByText(marker).count()) > 0
      check(`集成面板可切到 ${tab}`, seen, seen ? '' : `未见「${marker}」`)
    }
    await page.screenshot({ path: path.join(shotDir, '06-integrations.png') })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)

    section('⑬ 访问口令：登录与失效')
    // 另起一个带口令的实例，走一遍真实部署时的那条路：先登录，再让会话中途失效。
    const authPort = await freePort()
    const authBase = `http://127.0.0.1:${authPort}`
    const authLog = fs.openSync(path.join(tmp, 'auth-server.log'), 'w')
    const authServer = spawn(binary, ['-addr', `:${authPort}`, '-db', path.join(tmp, 'auth.db'), '-token', AUTH_TOKEN], {
      stdio: ['ignore', authLog, authLog],
    })
    try {
      await waitHealthy(authBase)
      // 预先标记当日仪式已完成：这里要验的是鉴权，不该被晨省浮层挡住。
      await fetch(`${authBase}/api/settings?token=${encodeURIComponent(AUTH_TOKEN)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ morningPlanDone: today, reviewDone: today }),
      })

      const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })
      const p2 = await ctx.newPage()
      const authErrors = []
      p2.on('console', (m) => {
        if (m.type() === 'error') authErrors.push(m.text())
      })
      p2.on('pageerror', (err) => authErrors.push(String(err)))

      await p2.goto(authBase, { waitUntil: 'domcontentloaded' })
      await p2.waitForTimeout(600)
      check('未登录时看到独立登录页', (await p2.locator('input[type=password]').count()) === 1)
      check('未登录时不加载应用（看不到侧栏）', (await p2.getByText('收集箱').count()) === 0)

      await p2.locator('input[type=password]').fill('wrong-token')
      await p2.locator('button[type=submit]').click()
      await p2.waitForTimeout(800)
      check('口令错误时给出提示', (await p2.getByText('口令不正确').count()) > 0)
      await p2.screenshot({ path: path.join(shotDir, '12-auth-login.png') })

      await p2.locator('input[type=password]').fill(AUTH_TOKEN)
      await p2.locator('button[type=submit]').click()
      await p2.waitForSelector('text=收集箱', { timeout: 15000 })
      check('口令正确后进入应用', (await p2.locator('aside').count()) > 0)
      check('登录后落到今天视图', (await p2.locator('h1').first().innerText()).includes('今天'))

      // 会话在页面开着的时候失效：清掉 Cookie，再触发一次写操作
      await ctx.clearCookies()
      const authQuick = p2.locator('#shenshi-quickadd')
      await authQuick.fill('会话失效后写入')
      await authQuick.press('Enter')
      await p2.waitForTimeout(1300)
      check('会话失效后弹出解锁界面', (await p2.locator('[data-lock-screen]').count()) === 1)
      await p2.screenshot({ path: path.join(shotDir, '13-auth-locked.png') })

      await p2.locator('#shenshi-token').fill(AUTH_TOKEN)
      await p2.locator('[data-lock-screen] button[type=submit]').click()
      await p2.waitForSelector('text=收集箱', { timeout: 15000 })
      check('重新输入口令后恢复正常', (await p2.locator('[data-lock-screen]').count()) === 0)
      const realAuthErrors = authErrors.filter((t) => !IGNORABLE.test(t) && !EXPECTED_AUTH_NOISE.test(t))
      check('解锁过程中无意外报错', realAuthErrors.length === 0, realAuthErrors.slice(0, 2).join(' | '))
      await ctx.close()
    } finally {
      authServer.kill('SIGTERM')
      await new Promise((r) => setTimeout(r, 300))
      if (!authServer.killed) authServer.kill('SIGKILL')
      fs.closeSync(authLog)
    }

    section('⑭ 运行时无错误')
    const realConsole = consoleErrors.filter((t) => !IGNORABLE.test(t))
    check('无控制台错误', realConsole.length === 0, realConsole.slice(0, 3).join(' | '))
    check('无未捕获异常', pageErrors.length === 0, pageErrors.slice(0, 3).join(' | '))

    console.log('\n' + '='.repeat(56))
    console.log(`通过 ${passed.length}/${passed.length + failed.length}`)
    console.log(`截图目录：${shotDir}`)
    if (failed.length) {
      console.log('失败项：')
      for (const f of failed) console.log(`  - ${f}`)
    } else {
      console.log('全部通过 ✓')
    }
    process.exitCode = failed.length ? 1 : 0
  } finally {
    if (browser) await browser.close()
    server.kill('SIGTERM')
    await new Promise((r) => setTimeout(r, 300))
    if (!server.killed) server.kill('SIGKILL')
    fs.closeSync(logFile)
  }
}

main().catch((err) => {
  console.error('\n执行失败：', err)
  process.exitCode = 1
})
