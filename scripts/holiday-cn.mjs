#!/usr/bin/env node
/**
 * 法定节假日数据的同步与校验工具。
 *
 * 农历与节气是确定性算法，自己实现即可，永远不需要更新；
 * 但「哪天连休、哪天补班」是国务院办公厅每年 11 月前后公布的，**必须外部供给**。
 * 本脚本对接 NateScarlet/holiday-cn（每日自动抓取国务院公告，CI 自动更新），
 * 直接维护 server/internal/lunar/holidays.json —— 那个文件由 go:embed 打进二进制。
 *
 *   --sync 2027            拉取该年数据并写入 holidays.json（可逗号分隔多年）
 *   --sync 2027 --dry-run  只打印将要写入的内容，不改文件
 *   --check 2027           与运行中的本地服务逐日核对，报出差异
 *
 * 每年 11 月国务院发文后的标准动作：
 *   node scripts/holiday-cn.mjs --sync 2028
 *   ./bin/shenshi -addr :8787 && node scripts/holiday-cn.mjs --check 2028
 *   cd server && go test ./internal/lunar/
 */

import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const DATA = path.join(HERE, '..', 'server', 'internal', 'lunar', 'holidays.json')
const BASE = 'https://cdn.jsdelivr.net/gh/NateScarlet/holiday-cn@master'
const API = process.env.SHENSHI_URL ?? 'http://127.0.0.1:8787'

function arg(flag) {
  const i = process.argv.indexOf(flag)
  return i >= 0 && process.argv[i + 1] && !process.argv[i + 1].startsWith('--') ? process.argv[i + 1] : null
}

const syncYears = arg('--sync')
const checkYear = arg('--check')
const dryRun = process.argv.includes('--dry-run')
if (!syncYears && !checkYear) {
  console.error('用法：node scripts/holiday-cn.mjs (--sync <年[,年]> [--dry-run] | --check <年>)')
  process.exit(1)
}

async function fetchYear(y) {
  const resp = await fetch(`${BASE}/${y}.json`)
  if (!resp.ok) throw new Error(`取 ${y}.json 失败：HTTP ${resp.status}`)
  const json = await resp.json()
  if (!Array.isArray(json.days) || json.days.length === 0) {
    throw new Error(`${y} 年暂无数据（国务院公告尚未发布，或 holiday-cn 还没抓到）`)
  }
  return json
}

/** 逐日对象各自占一行：JSON.stringify 会把每个对象摊成 5 行，改起来没法看。 */
function render(data) {
  const years = Object.keys(data.years).sort()
  const lines = ['{', `  "source": ${JSON.stringify(data.source)},`, '  "years": {']
  years.forEach((y, yi) => {
    const v = data.years[y]
    lines.push(`    ${JSON.stringify(y)}: {`)
    lines.push(`      "papers": ${JSON.stringify(v.papers ?? [])},`)
    lines.push('      "days": [')
    v.days.forEach((d, di) => {
      const tail = di === v.days.length - 1 ? '' : ','
      lines.push(`        { "date": ${JSON.stringify(d.date)}, "name": ${JSON.stringify(d.name)}, "isOffDay": ${d.isOffDay} }${tail}`)
    })
    lines.push('      ]')
    lines.push(`    }${yi === years.length - 1 ? '' : ','}`)
  })
  lines.push('  }', '}', '')
  return lines.join('\n')
}

// ---------- --sync：写入 holidays.json ----------
async function sync(spec) {
  const wanted = spec.split(',').map((s) => s.trim()).filter(Boolean)
  for (const y of wanted) if (!/^\d{4}$/.test(y)) throw new Error(`年份格式不对：${y}`)

  let data = { source: 'https://github.com/NateScarlet/holiday-cn', years: {} }
  if (fs.existsSync(DATA)) {
    data = JSON.parse(fs.readFileSync(DATA, 'utf8'))
    data.years ??= {}
  }

  // 单年取不到（国务院还没发文）只跳过，不算失败 —— 定时任务每周都会带上「次年」，
  // 若因此整体失败，就会出现「每周都红、但其实没事」的假警报。
  const done = []
  const skipped = []
  for (const y of wanted) {
    let json
    try {
      json = await fetchYear(y)
    } catch (e) {
      skipped.push(`${y}（${e.message}）`)
      continue
    }
    const days = [...json.days].sort((a, b) => a.date.localeCompare(b.date))
    data.years[y] = { papers: json.papers ?? [], days }
    done.push(`${y}：${days.length} 天`)
    if (json.papers?.length) console.log(`  ${y} 原文 ${json.papers[0]}`)
  }
  if (done.length === 0) throw new Error(`没有取到任何一年的数据：${skipped.join('；')}`)
  for (const s of skipped) console.log(`  ⚠ 跳过 ${s}`)
  data.source = 'https://github.com/NateScarlet/holiday-cn'

  const text = render(data)
  if (dryRun) {
    console.log(text)
    console.log(`（--dry-run，未写文件）`)
    return
  }
  fs.writeFileSync(DATA, text)
  console.log(`✅ 已写入 ${path.relative(process.cwd(), DATA)}：${done.join('，')}`)
  console.log('   该文件由 go:embed 打进二进制，重新 build 后生效。')
}

// ---------- --check：与本地服务逐日核对 ----------
async function check(y) {
  const json = await fetchYear(y)
  const resp = await fetch(`${API}/api/meta/calendar?from=${y}-01-01&to=${y}-12-31`)
  if (!resp.ok) throw new Error(`读本地服务失败：HTTP ${resp.status}。先启动 ./bin/shenshi -addr :8787`)
  const mine = new Map((await resp.json()).days.map((d) => [d.date, d]))

  const bad = []
  for (const d of json.days) {
    const m = mine.get(d.date)
    if (!m) {
      bad.push(`${d.date} 本地缺数据`)
      continue
    }
    // holiday-cn 的 isOffDay 就是「这天放不放假」，与 isRest 一一对应。
    if (d.isOffDay && !m.isRest) bad.push(`${d.date} 应放假，本地为 ${m.kind}`)
    if (!d.isOffDay && m.isRest) bad.push(`${d.date} 应上班，本地为 ${m.kind}`)
    if (d.isOffDay && !m.holidaySpan) bad.push(`${d.date} 缺所属假期名（应为「${d.name}」）`)
  }

  // 反向扫：本地标了「非周末却放假 / 周末却上班」的日子，官方必须也列出来，
  // 否则说明本地多标了（多半是区间写宽）。
  const listed = new Set(json.days.map((d) => d.date))
  for (const m of mine.values()) {
    const w = new Date(m.date + 'T00:00:00').getDay()
    const weekend = w === 0 || w === 6
    if (m.isRest && !weekend && !listed.has(m.date)) bad.push(`${m.date} 本地标放假，官方未列（${m.kind}）`)
    if (!m.isRest && weekend && !listed.has(m.date)) bad.push(`${m.date} 本地标上班，官方未列（${m.kind}）`)
  }

  console.log(`官方数据 ${json.days.length} 天（${json.papers?.[0] ?? '无原文链接'}）`)
  if (bad.length === 0) {
    console.log('✅ 0 处不一致')
    return
  }
  console.log(`❌ ${bad.length} 处不一致：`)
  for (const b of bad.slice(0, 40)) console.log('  ' + b)
  process.exitCode = 1
}

;(syncYears ? sync(syncYears) : check(checkYear)).catch((e) => {
  console.error('失败：' + e.message)
  process.exit(1)
})
