#!/usr/bin/env node
/**
 * 自然语言解析器的回归检查。
 *
 * 解析器是全项目逻辑最密的一块，且出错时不会有任何报错，只会安静地解析错日期。
 * 因此这里用固定「今天」跑一批确定性用例：先把 TS 编译到临时目录，再逐条断言。
 *
 * 用法：node scripts/check-nlp.mjs
 */

import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '..')
const outDir = mkdtempSync(path.join(tmpdir(), 'shenshi-nlp-'))

// 用项目自带的打包器把 TS 打成 CJS，再直接 require 进来跑断言。
// 这样无需引入测试框架，也不依赖 ts-node 之类的额外工具链。
const ROLldown = path.join(webRoot, 'node_modules', '.bin', 'rolldown')
const bundleFile = path.join(outDir, 'nlp.cjs')
execFileSync(
  ROLldown,
  [path.join('src', 'lib', 'nlp.ts'), '-o', bundleFile, '-f', 'cjs', '-p', 'node'],
  { cwd: webRoot, stdio: 'inherit' },
)

const require = createRequire(import.meta.url)
const { parseQuickAdd, describeRepeat } = require(bundleFile)

// 固定基准日：2026-09-22 是周二。
const TODAY = '2026-09-22'
const LISTS = [
  { id: 7, name: '工作' },
  { id: 8, name: '个人事务' },
]

let pass = 0
const failures = []

function check(name, input, expected) {
  const got = parseQuickAdd(input, { today: TODAY, lists: LISTS })
  const actual = {}
  for (const k of Object.keys(expected)) actual[k] = got[k]
  const ok = Object.entries(expected).every(([k, v]) => JSON.stringify(actual[k]) === JSON.stringify(v))
  if (ok) {
    pass++
    console.log(`  \u001b[32m✓\u001b[0m ${name}`)
  } else {
    failures.push(`${name}\n      输入: ${input}\n      期望: ${JSON.stringify(expected)}\n      实际: ${JSON.stringify(actual)}`)
    console.log(`  \u001b[31m✗\u001b[0m ${name}`)
  }
}

console.log(`基准日 ${TODAY}（周二）\n`)

console.log('日期识别')
check('明天', '明天下午3点开会', { dueDate: '2026-09-23', dueTime: '15:00', title: '开会' })
check('后天', '后天交材料', { dueDate: '2026-09-24', title: '交材料' })
check('大后天', '大后天体检', { dueDate: '2026-09-25', title: '体检' })
check('裸周X（本周五）', '周五复盘', { dueDate: '2026-09-25', title: '复盘' })
check('本周日', '本周日休息', { dueDate: '2026-09-27', title: '休息' })
check('下周一', '下周一提交周报', { dueDate: '2026-09-28', title: '提交周报' })
check('N 天后', '3天后回访客户', { dueDate: '2026-09-25', title: '回访客户' })
check('两周后', '两周后归档', { dueDate: '2026-10-06', title: '归档' })
check('月日', '9月30日交材料', { dueDate: '2026-09-30', title: '交材料' })
check('跨月排期', '10月8日汇报', { dueDate: '2026-10-08', title: '汇报' })
check('完整年月日', '2026年11月3日述职', { dueDate: '2026-11-03', title: '述职' })
check('未来「15号」', '15号缴费', { dueDate: '2026-10-15', title: '缴费' })
check('本月内「25号」', '25号交材料', { dueDate: '2026-09-25', title: '交材料' })
check('月底', '月末汇总', { dueDate: '2026-09-30', title: '汇总' })
check('下月某日', '下个月5号启动', { dueDate: '2026-10-05', title: '启动' })
check('ISO 日期 + 时间', '2026-10-01 09:30 升旗', { dueDate: '2026-10-01', dueTime: '09:30', title: '升旗' })
check('斜杠不被当作日期', '阅读 3/5 章节', { dueDate: null, title: '阅读 3/5 章节' })
check('范围连字符不被当作日期', '3-5 天完成初稿', { dueDate: null, title: '3-5 天完成初稿' })

console.log('\n时间识别')
check('中文点位', '下午三点半喝茶', { dueTime: '15:30', title: '喝茶' })
check('晚上点位', '晚上8点复盘', { dueTime: '20:00', title: '复盘' })
check('无时段按字面', '3点开会', { dueTime: '03:00', title: '开会' })
check('冒号时间', '明天 18:00 健身', { dueDate: '2026-09-23', dueTime: '18:00', title: '健身' })
check('时段带冒号', '下午3:30过一下方案', { dueTime: '15:30', title: '过一下方案' })
check('全角冒号', '早上9：05晨读', { dueTime: '09:05', title: '晨读' })
check('仅时段词用默认时刻', '下午过审', { dueTime: '14:00', title: '过审' })
check('凌晨12点归零', '凌晨12点值守', { dueTime: '00:00', title: '值守' })

console.log('\n重复规则')
check('每天', '每天早上8点读书', { repeatRule: 'daily', dueTime: '08:00', title: '读书' })
check('每个工作日', '每个工作日写日志', { repeatRule: 'weekdays', title: '写日志' })
check('每周', '读书 每周', { repeatRule: 'weekly', title: '读书' })
check('每周某几日', '每周一、三、五晨跑', { repeatRule: 'weekly:1,3,5', title: '晨跑' })
check('每月某日', '每月15日交房租', { repeatRule: 'monthly:15', title: '交房租' })
check('每月最后一天', '每月最后一天结账', { repeatRule: 'monthly:last', title: '结账' })
check('每隔N天', '每2天浇花', { repeatRule: 'every:2:day', title: '浇花' })
check('艾宾浩斯', '复习笔记 艾宾浩斯', { repeatRule: 'ebbinghaus:0', title: '复习笔记' })

console.log('\n标签 / 清单 / 优先级 / 四象限')
check('标签', '写方案 #工作', { tagNames: ['工作'], title: '写方案' })
check('多标签', '写方案 #工作 #本周', { tagNames: ['工作', '本周'], title: '写方案' })
check('清单', '整理灵感 /工作', { listId: 7, title: '整理灵感' })
check('列表名不存在则不改标题', '阅读 3/5 章节', { listId: null, title: '阅读 3/5 章节' })
check('感叹号数量', '写方案 !!!', { priority: 3, title: '写方案' })
check('中文优先级', '写方案 !高', { priority: 3, title: '写方案' })
check('数字优先级', '写方案 !2', { priority: 2, title: '写方案' })
check('四象限', '评审 @重要 @紧急', { important: true, urgent: true, title: '评审' })

console.log('\n全角符号（中文输入法下不切半角也认）')
check('全角标签', '写方案 ＃工作', { tagNames: ['工作'], title: '写方案' })
check('全角清单', '整理灵感 ／工作', { listId: 7, title: '整理灵感' })
check('全角清单名不存在不改标题', '阅读 3／5 章节', { listId: null, title: '阅读 3／5 章节' })
check('全角优先级词', '写方案 ！高', { priority: 3, title: '写方案' })
check('全角感叹号数量', '写方案 ！！！', { priority: 3, title: '写方案' })
check('全半角感叹号混写计数', '写方案 ！！!', { priority: 3, title: '写方案' })
check('全角数字优先级', '写方案 ！2', { priority: 2, title: '写方案' })
check('全角四象限', '评审 ＠重要 ＠紧急', { important: true, urgent: true, title: '评审' })
check(
  '全角符号混排',
  '明天下午3点开会 ＃工作 ！高 ／工作',
  { dueDate: '2026-09-23', dueTime: '15:00', tagNames: ['工作'], priority: 3, listId: 7, title: '开会' },
)
check('正文里的全角感叹号不被吃掉', '写「慎始」的验收清单！', { priority: null, title: '写「慎始」的验收清单！' })

console.log('\n组合与边界')
check(
  '多要素混排',
  '明天下午3点开会 #工作 !高 /工作 每天',
  {
    dueDate: '2026-09-23',
    dueTime: '15:00',
    tagNames: ['工作'],
    priority: 3,
    repeatRule: 'daily',
    listId: 7,
    title: '开会',
  },
)
check(
  '要素顺序无关',
  '#工作 !高 明天下午3点 开会',
  { dueDate: '2026-09-23', dueTime: '15:00', tagNames: ['工作'], priority: 3, title: '开会' },
)
check('纯标题不受影响', '看一部电影', {
  dueDate: null,
  dueTime: null,
  repeatRule: null,
  priority: null,
  title: '看一部电影',
})
check('数字标题不被误吃', '完成 3 个模块', { dueDate: null, dueTime: null, title: '完成 3 个模块' })
check('标题保留正文标点', '写「慎始」的验收清单', { title: '写「慎始」的验收清单' })

console.log('\n重复规则描述')
for (const [rule, expect] of [
  ['daily', '每天'],
  ['weekdays', '每个工作日'],
  ['weekly:1,3,5', '每周一、三、五'],
  ['monthly:last', '每月最后一天'],
  ['monthly:15', '每月 15 日'],
  ['every:2:week', '每 2 周'],
  ['ebbinghaus:2', '艾宾浩斯（第 3 轮，间隔 4 天）'],
]) {
  const got = describeRepeat(rule)
  if (got === expect) {
    pass++
    console.log(`  \u001b[32m✓\u001b[0m ${rule} → ${got}`)
  } else {
    failures.push(`describeRepeat(${rule}) 期望「${expect}」实际「${got}」`)
    console.log(`  \u001b[31m✗\u001b[0m ${rule} → ${got}（期望 ${expect}）`)
  }
}

rmSync(outDir, { recursive: true, force: true })

console.log(`\n${'='.repeat(52)}`)
if (failures.length) {
  console.log(`通过 ${pass}，失败 ${failures.length}\n`)
  for (const f of failures) console.log(`  - ${f}\n`)
  process.exit(1)
}
console.log(`\u001b[32m全部通过 ${pass}/${pass}\u001b[0m`)
