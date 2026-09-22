/**
 * 备注用的极简 Markdown 渲染器。
 *
 * 之所以自己写而不引第三方库：备注要支持的语法就那么几样（标题、列表、引用、
 * 代码、链接、强调），为此背一个完整的 CommonMark 实现不划算。更重要的是安全
 * 边界要握在自己手里 —— 备注可能来自任何地方，渲染结果是要塞进 DOM 的。
 *
 * 安全口径：**先整体转义 HTML，再做 Markdown 替换**。任何 `<script>`、事件属性
 * 在转义后都只是文本；链接协议另有白名单，挡掉 javascript: 与 data:。
 */

const ALLOWED_PROTOCOLS = ['http:', 'https:', 'mailto:']

export function renderMarkdown(src: string): string {
  if (!src) return ''
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const out: string[] = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    // 代码块：成对的 ``` 之间原样保留（内容已转义）
    if (/^\s*```/.test(line)) {
      const lang = line.replace(/^\s*```/, '').trim()
      const buf: string[] = []
      i++
      while (i < lines.length && !/^\s*```/.test(lines[i])) {
        buf.push(escapeHTML(lines[i]))
        i++
      }
      i++ // 收掉收尾的 ```
      const cls = lang ? ` class="md-code" data-lang="${escapeHTML(lang)}"` : ' class="md-code"'
      out.push(`<pre${cls}><code>${buf.join('\n')}</code></pre>`)
      continue
    }

    // 空行只作分隔
    if (line.trim() === '') {
      i++
      continue
    }

    // 分隔线
    if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      out.push('<hr />')
      i++
      continue
    }

    // 标题
    const heading = /^(#{1,6})\s+(.*)$/.exec(line)
    if (heading) {
      const level = heading[1].length
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`)
      i++
      continue
    }

    // 引用：连续的 > 行合成一块
    if (/^\s*>\s?/.test(line)) {
      const buf: string[] = []
      while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
        buf.push(inline(lines[i].replace(/^\s*>\s?/, '')))
        i++
      }
      out.push(`<blockquote>${buf.join('<br />')}</blockquote>`)
      continue
    }

    // 列表：无序、有序、任务项统一按块处理
    if (isListItem(line)) {
      const { html, next } = renderList(lines, i)
      out.push(html)
      i = next
      continue
    }

    // 段落：吃连续的非空行，遇到块级起始就收尾
    const buf: string[] = []
    while (i < lines.length && lines[i].trim() !== '' && !startsBlock(lines[i])) {
      buf.push(inline(lines[i]))
      i++
    }
    out.push(`<p>${buf.join('<br />')}</p>`)
  }

  return out.join('')
}

function startsBlock(line: string): boolean {
  return (
    /^\s*```/.test(line) ||
    /^\s*#{1,6}\s+/.test(line) ||
    /^\s*>\s?/.test(line) ||
    isListItem(line) ||
    /^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)
  )
}

function isListItem(line: string): boolean {
  return /^\s*[-*+]\s+/.test(line) || /^\s*\d+[.)]\s+/.test(line)
}

/** 渲染一个列表块，支持单层嵌套（缩进 2 空格以上）。 */
function renderList(lines: string[], start: number): { html: string; next: number } {
  const ordered = /^\s*\d+[.)]\s+/.test(lines[start])
  const items: string[] = []
  let i = start

  while (i < lines.length) {
    const line = lines[i]
    if (!isListItem(line)) {
      // 缩进的续行归到上一个条目
      if (items.length > 0 && /^\s{2,}\S/.test(line) && line.trim() !== '') {
        items[items.length - 1] += '<br />' + inline(line.trim())
        i++
        continue
      }
      break
    }
    const m = /^\s*(?:[-*+]|\d+[.)])\s+(.*)$/.exec(line)
    const body = m ? m[1] : ''
    const task = /^\[( |x|X)\]\s+(.*)$/.exec(body)
    if (task) {
      const checked = task[1].toLowerCase() === 'x'
      items.push(
        `<li class="md-task"><span class="md-box">${checked ? '✓' : ''}</span>${inline(task[2])}</li>`,
      )
    } else {
      items.push(`<li>${inline(body)}</li>`)
    }
    i++
  }

  const tag = ordered ? 'ol' : 'ul'
  const cls = items.some((it) => it.includes('md-task')) ? ' class="md-tasklist"' : ''
  return { html: `<${tag}${cls}>${items.join('')}</${tag}>`, next: i }
}

/** 行内语法。入参是原文，转义与替换都在这里完成。 */
function inline(text: string): string {
  const codes: string[] = []
  // 行内代码先抽出来占位，免得里面的 * _ 被当成强调
  let s = text.replace(/`([^`]+)`/g, (_m, code: string) => {
    codes.push(`<code>${escapeHTML(code)}</code>`)
    return `\u0000${codes.length - 1}\u0000`
  })

  s = escapeHTML(s)

  // 链接：[文字](地址)
  s = s.replace(/\[([^\]]*)\]\(([^)\s]+)\)/g, (m, label: string, url: string) => {
    const safe = safeURL(url)
    if (!safe) return m
    return `<a href="${safe}" target="_blank" rel="noopener noreferrer">${label || safe}</a>`
  })

  // 裸链接
  s = s.replace(/(https?:\/\/[^\s<]+)/g, (m, url: string) => {
    const safe = safeURL(url)
    if (!safe) return m
    return `<a href="${safe}" target="_blank" rel="noopener noreferrer">${url}</a>`
  })

  s = s
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/__([^_]+)__/g, '<strong>$1</strong>')
    .replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>')
    .replace(/(^|[^_])_([^_\n]+)_/g, '$1<em>$2</em>')
    .replace(/~~([^~]+)~~/g, '<del>$1</del>')

  // 还原行内代码
  s = s.replace(/\u0000(\d+)\u0000/g, (_m, idx: string) => codes[Number(idx)] ?? '')
  return s
}

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/** 只放行白名单协议，其余一律返回 null（调用方原样输出文本）。 */
function safeURL(raw: string): string | null {
  const url = raw.trim()
  if (/^[a-z][a-z0-9+.-]*:/i.test(url)) {
    try {
      const parsed = new URL(url)
      if (!ALLOWED_PROTOCOLS.includes(parsed.protocol)) return null
    } catch {
      return null
    }
    return url.replace(/"/g, '%22')
  }
  // 相对地址（如 /api/...）不对外开放：备注里没必要跳站内
  return null
}

/** 粗略判断是否值得走 Markdown 渲染：纯文本直接按段落展示更省事。 */
export function looksLikeMarkdown(s: string): boolean {
  return /(^|\n)\s*(#{1,6}\s|>|[-*+]\s|\d+[.)]\s|```)|(\*\*|~~|`|\[[^\]]*\]\()/.test(s)
}
