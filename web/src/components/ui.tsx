/** 通用 UI 基元：按钮、弹层、勾选框、空状态。全部无第三方依赖。 */

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'

import { useEscapeLayer } from '../lib/escStack'
import { popModalLayer, pushModalLayer } from '../lib/modalLayer'
import { IconCheck, IconX, type IconProps } from './icons'

/** 可聚焦元素选择器：与浏览器的 Tab 顺序口径保持一致（排除 disabled 与 tabindex=-1）。 */
const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ')
}

/* ---------------- 按钮 ---------------- */

type ButtonVariant = 'primary' | 'ghost' | 'subtle' | 'danger' | 'outline'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  icon?: (p: IconProps) => ReactNode
  iconRight?: (p: IconProps) => ReactNode
  size?: 'sm' | 'md'
}

const VARIANTS: Record<ButtonVariant, string> = {
  primary:
    'bg-seal text-seal-contrast hover:brightness-110 active:brightness-95 shadow-[0_1px_2px_rgb(0_0_0/0.12)]',
  outline: 'border border-control-line text-ink hover:bg-surface-2',
  ghost: 'text-ink-2 hover:bg-surface-2 hover:text-ink',
  subtle: 'bg-surface-2 text-ink hover:bg-surface-3',
  danger: 'bg-p-high text-white hover:brightness-110',
}

export function Button({
  variant = 'ghost',
  icon: Icon,
  iconRight: IconRight,
  size = 'md',
  className,
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      className={cx(
        'inline-flex items-center justify-center gap-1.5 rounded-lg font-medium transition-all duration-150',
        'disabled:cursor-not-allowed disabled:opacity-45',
        size === 'sm' ? 'h-7 px-2.5 text-[0.78125rem]' : 'h-8.5 px-3 text-[0.8125rem]',
        VARIANTS[variant],
        className,
      )}
      {...rest}
    >
      {Icon ? <Icon size={size === 'sm' ? 13 : 15} /> : null}
      {children}
      {IconRight ? <IconRight size={size === 'sm' ? 13 : 15} /> : null}
    </button>
  )
}

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: (p: IconProps) => ReactNode
  label: string
  active?: boolean
  size?: number
  tone?: 'default' | 'danger'
}

export function IconButton({ icon: Icon, label, active, size = 15, tone, className, ...rest }: IconButtonProps) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      className={cx(
        // 窄屏（<lg）触控热区放大到 36px（WCAG 2.5.8 目标尺寸），桌面维持 28px 的紧凑排布
        'inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg transition-colors duration-150 lg:h-7 lg:w-7',
        active ? 'bg-seal/12 text-seal' : 'text-ink-3 hover:bg-surface-2 hover:text-ink',
        tone === 'danger' && 'hover:text-p-high',
        className,
      )}
      {...rest}
    >
      <Icon size={size} />
    </button>
  )
}

/* ---------------- 勾选框 ---------------- */

export function RoundCheck({
  checked,
  onChange,
  color,
  size = 18,
  title,
  disabled,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  color?: string
  size?: number
  title?: string
  /** 请求在途时置灰并阻断重入（打卡这类操作连点会产生"记一次 + 撤一次"的竞态）。 */
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={title ?? (checked ? '标记为未完成' : '标记为已完成')}
      aria-pressed={checked}
      onClick={(e) => {
        e.stopPropagation()
        onChange(!checked)
      }}
      className={cx(
        'group/check relative grid shrink-0 place-items-center rounded-full border transition-all duration-200',
        checked ? 'border-transparent' : 'border-control-line hover:border-seal',
        disabled && 'cursor-not-allowed opacity-50',
      )}
      style={{
        width: size,
        height: size,
        background: checked ? color || 'var(--seal)' : 'transparent',
      }}
    >
      <IconCheck
        size={size - 6}
        className={cx('text-white transition-opacity duration-150', checked ? 'opacity-100' : 'opacity-0')}
        strokeWidth={2.6}
      />
    </button>
  )
}

export function Checkbox({
  checked,
  indeterminate,
  onChange,
  label,
}: {
  checked: boolean
  indeterminate?: boolean
  onChange: (next: boolean) => void
  label?: string
}) {
  return (
    <button
      type="button"
      aria-checked={checked}
      role="checkbox"
      onClick={(e) => {
        e.stopPropagation()
        onChange(!checked)
      }}
      className="inline-flex items-center gap-2 text-[0.8125rem] text-ink-2"
    >
      <span
        className={cx(
          'grid h-4 w-4 place-items-center rounded-[5px] border transition-colors',
          checked || indeterminate ? 'border-seal bg-seal text-white' : 'border-control-line',
        )}
      >
        {checked ? <IconCheck size={11} strokeWidth={3} /> : indeterminate ? <span className="h-0.5 w-2 rounded bg-white" /> : null}
      </span>
      {label}
    </button>
  )
}

/* ---------------- 弹层 ---------------- */

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  width = 560,
}: {
  open: boolean
  onClose: () => void
  title: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: number
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const restoreRef = useRef<HTMLElement | null>(null)
  const labelledById = useId()

  // 弹层计数：应用主内容据此进入 inert（不可聚焦、不被读屏读到）
  useEffect(() => {
    if (!open) return
    pushModalLayer()
    return popModalLayer
  }, [open])

  // Esc：注册到仲裁栈（栈顶优先），**不依赖焦点位置**。
  // 早先是在对话框容器上监听 keydown —— 只有焦点仍在弹窗内才收得到。
  // 而焦点是会跑掉的：最典型的是点了行内「完成」，那一行随即消失，
  // 焦点落回 <body>，此后 Esc 无人接管，弹窗关不掉（2026-09-24 CI 冒烟实测）。
  useEscapeLayer(open, onClose)

  // 焦点管理：打开时把焦点移入弹窗；关闭时归还给触发它的元素
  useEffect(() => {
    if (!open) return
    const active = document.activeElement
    restoreRef.current = active instanceof HTMLElement ? active : null
    const el = dialogRef.current
    if (!el) return
    // 初始焦点：显式声明优先，其次带 autoFocus 的输入框，最后落到对话框本身。
    // 不再默认聚焦「第一个可聚焦元素」—— 那通常正是右上角的关闭按钮，
    // 一回车就把弹窗关掉了；聚焦 dialog 容器则会先朗读标题与角色。
    const target =
      el.querySelector<HTMLElement>('[data-autofocus]') ??
      el.querySelector<HTMLElement>('[autofocus]') ??
      el
    // 让出一帧，避开入场动画首帧的布局抖动
    const timer = window.setTimeout(() => target.focus(), 0)
    return () => {
      window.clearTimeout(timer)
      const back = restoreRef.current
      restoreRef.current = null
      // 此刻背景可能还挂着 inert（它由同一轮 effect 里的弹层计数触发重渲染后才移除），
      // 对 inert 子树内的元素调 focus() 会被浏览器忽略 —— 让出一拍再归还。
      if (back && document.contains(back)) window.setTimeout(() => back.focus(), 0)
    }
  }, [open])

  const onKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      // Esc 不在这里处理 —— 统一交给上面 useEscapeLayer 注册的仲裁栈。
      // 这里只留焦点陷阱：Tab / Shift+Tab 在弹窗内循环。
      if (e.key !== 'Tab') return
      const el = dialogRef.current
      if (!el) return
      const nodes = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (n) => n.offsetParent !== null || n === document.activeElement,
      )
      if (nodes.length === 0) {
        e.preventDefault()
        el.focus()
        return
      }
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      const activeEl = document.activeElement as HTMLElement | null
      const inside = !!activeEl && el.contains(activeEl)
      if (e.shiftKey) {
        if (!inside || activeEl === first) {
          e.preventDefault()
          last.focus()
        }
      } else if (!inside || activeEl === last) {
        e.preventDefault()
        first.focus()
      }
    },
    [],
  )

  if (!open) return null
  // 垂直居中定位。早先是 `items-start` + `py-[5vh]`：弹窗从视口顶部下移 5vh 就开始排布，
  // 内容少的对话框（删除确认、新建分组等）看上去明显「贴顶」，且视口越高越显偏上。
  // 改为居中后位置由剩余空间均分决定，与内容高度、视口高度都解耦。
  // 上下留白 1.5rem（略大于左右），避免高内容弹窗顶到视口边缘。
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center px-4 py-6">
      <div
        className="fixed inset-0 bg-black/28 backdrop-blur-[2px] animate-fade-in"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledById}
        tabIndex={-1}
        onKeyDown={onKeyDown}
        className="relative flex max-h-full w-full animate-pop flex-col overflow-hidden rounded-2xl border border-line bg-surface shadow-[var(--shadow-lg)] outline-none"
        style={{ maxWidth: `min(${width}px, calc(100vw - 2rem))` }}
      >
        <header className="flex shrink-0 items-start justify-between gap-4 border-b border-line px-5 py-4">
          <div>
            <h2 id={labelledById} className="brand-serif text-[1.0625rem] font-semibold tracking-wide text-ink">
              {title}
            </h2>
            {subtitle ? <p className="mt-0.5 text-[0.78125rem] text-ink-3">{subtitle}</p> : null}
          </div>
          <IconButton icon={IconX} label="关闭" onClick={onClose} />
        </header>
        {/* 正文区随内容自适应：内容少时保持紧凑，内容多时撑满可用高度再滚动。
            整个弹窗最高占满视口（减去上下留白），不再用固定 vh 卡死内容。 */}
        <div className="min-h-0 flex-auto overflow-y-auto px-5 py-4">{children}</div>
        {footer ? (
          <footer className="flex shrink-0 items-center justify-end gap-2 border-t border-line bg-surface-2/60 px-5 py-3">
            {footer}
          </footer>
        ) : null}
      </div>
    </div>,
    document.body,
  )
}

/** 轻量浮层：点击外部或按 Esc 关闭。 */
export function Popover({
  open,
  onClose,
  children,
  align = 'left',
  side = 'bottom',
  width,
  className,
  menu = false,
}: {
  open: boolean
  onClose: () => void
  children: ReactNode
  align?: 'left' | 'right' | 'center'
  side?: 'bottom' | 'top'
  width?: number
  className?: string
  /**
   * 菜单型浮层：打开时把焦点落到第一项，关闭时归还给触发元素。
   * 默认关闭 —— 快速添加的候选列表正相反，抢走 input 焦点会打断输入。
   */
  menu?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)

  // 实际生效的方向、高度上限与落点。
  // 浮层用 fixed + portal 挂到 body，尺寸只有打开后才量得到；行靠近视口底部时
  // 向下展开会溢出视口（要滚动页面才看得到），而上方的空白常常还很充裕。
  // 打开时量一次，按四档决策：本侧放得下 → 不动；另一侧放得下 → 翻转；
  // 两侧都放不下但整屏装得下 → 贴住空间更大一侧的视口边界一次显示完（不加滚动条）；
  // 整屏也装不下 → 收窄 + 内部滚动（同原生 <select> 的弹层）。
  //
  // 为什么必须 `fixed` + `createPortal(…, document.body)`：
  // 1) 任务列表等容器是 `overflow-y-auto` 的滚动区，`absolute` 浮层会被它裁剪 ——
  //    向上翻转后超出容器上沿的部分直接不渲染（DOM 量着 696px 完整、渲染只剩半截）；
  // 2) 只改 fixed 还不够：`Toolbar` 的 header 带 `backdrop-blur`，`filter` 类属性会
  //    让后代 fixed 元素以它为包含块，于是"视口坐标"被整体偏移（实测 left 写 1141、
  //    实际落在 1460，飞出视口）。
  // 挂到 body 之下，两件事一起解决：祖先的 overflow 与 filter 都管不着它。
  //
  // 锚点取原位占位元素的 parentElement —— portal 之后面板的 parentElement 是 body，
  // 拿不到"触发按钮所在的那个容器"，故在渲染位置留一个 display:none 的标记。
  const [pos, setPos] = useState<{
    side: 'bottom' | 'top'
    maxHeight?: number
    /** 视口坐标（fixed 定位），未量测到之前为 undefined */
    top?: number
    left?: number
  }>({ side })
  const hostRef = useRef<HTMLSpanElement>(null)

  useLayoutEffect(() => {
    if (!open) return
    const el = ref.current
    if (!el) return
    const place = () => {
      // fixed 元素的 offsetParent 恒为 null，锚点取原位标记的父元素
      //（调用方把 Popover 放在触发按钮旁边，父元素通常就是按钮的包裹容器）。
      const anchor = hostRef.current?.parentElement ?? null
      const a = anchor?.getBoundingClientRect()
      if (!a) return
      const GAP = 6
      const EDGE = 8 // 与视口边缘的安全距离
      // 底部状态栏是布局内的常驻行（不是浮层），浮层停到它下面等于被它遮住，让出来。
      const footer = document.querySelector<HTMLElement>('[data-app-footer]')
      const topLimit = EDGE
      const bottomLimit = window.innerHeight - (footer?.offsetHeight ?? 0) - EDGE
      // 触发元素可能只露出一部分、甚至完全滚出视口：两侧空间一律以视口为界先夹紧，
      // 否则会把屏幕外的空隙算成可用空间，菜单被摆到视口外（实测踩过：700 高的视口里
      // 菜单落到了 714，压在状态栏外面）。
      const anchorTop = Math.min(Math.max(a.top, topLimit), bottomLimit)
      const anchorBottom = Math.min(Math.max(a.bottom, topLimit), bottomLimit)
      const below = Math.max(0, bottomLimit - anchorBottom - GAP)
      const above = Math.max(0, anchorTop - GAP - topLimit)
      const band = bottomLimit - topLimit // 视口整体可用的纵向空间
      // scrollHeight 不受 maxHeight 影响，始终是内容完整高度（外加上下各 1px 边框）
      const natural = el.scrollHeight + 2
      const space = (s: 'bottom' | 'top') => (s === 'bottom' ? below : above)
      const firstSpace = space(side)
      const other: 'bottom' | 'top' = side === 'bottom' ? 'top' : 'bottom'
      const otherSpace = space(other)
      const roomier = otherSpace > firstSpace ? other : side

      let chosen: 'bottom' | 'top'
      let maxHeight: number | undefined
      if (natural <= firstSpace) {
        chosen = side // 首选侧本来就放得下，不打扰
      } else if (natural <= otherSpace) {
        chosen = other // 翻到另一侧即可完整显示
      } else if (natural <= band) {
        // 两侧都放不下、但整屏装得下：贴住空间更大那一侧的视口边界，一次显示完。
        // 这里**不**收窄加滚动条 —— 屏幕明明还有位置，把内容藏进滚动区是下策
        //（用户反馈点）。代价是菜单会盖住触发元素一部分：菜单比两侧空隙都高，
        // 又必须整体可见，这个重叠避不开。
        chosen = roomier
      } else {
        // 整屏也装不下，只能在空间更大的一侧收窄并内部滚动。
        chosen = roomier
        maxHeight = space(chosen)
      }
      // 统一按「贴边 + 夹紧」求落点：normal 情况下就是紧贴触发元素的 GAP 处，
      // 空间不足时自然退化为贴住该侧的视口边界，且任何分支都不会落到视口外。
      // （anchor 已先按视口夹紧，故触发元素半露/滚出视口时也算得对。）
      const height = maxHeight ?? natural
      const unclamped = chosen === 'bottom' ? anchorBottom + GAP : anchorTop - GAP - height
      const top = Math.round(Math.min(Math.max(unclamped, topLimit), bottomLimit - height))
      // 水平位置也显式算：fixed 之后 right-0 / left-1/2 这些类是相对视口而非锚点，
      // 语义会错。顺带夹紧左右边缘，窄屏下不会有一半伸到屏幕外。
      const w = el.offsetWidth
      const rawLeft =
        align === 'right'
          ? a.right - w
          : align === 'center'
            ? a.left + a.width / 2 - w / 2
            : a.left
      const left = Math.round(Math.min(Math.max(rawLeft, EDGE), Math.max(EDGE, window.innerWidth - w - EDGE)))
      const next = { side: chosen, maxHeight, top, left }
      // 值相同就回传原对象 → React 跳过更新，避免与 ResizeObserver 互相触发
      setPos((p) =>
        p.side === next.side &&
        p.maxHeight === next.maxHeight &&
        p.top === next.top &&
        p.left === next.left
          ? p
          : next,
      )
    }
    let raf = 0
    const schedule = () => {
      if (raf) return
      raf = requestAnimationFrame(() => {
        raf = 0
        place()
      })
    }
    place()
    // 内容高度会变（候选列表随输入增减），滚动与缩放也会改变可用空间 —— 都要重算。
    const ro = new ResizeObserver(schedule)
    ro.observe(el)
    window.addEventListener('scroll', schedule, true)
    window.addEventListener('resize', schedule)
    return () => {
      if (raf) cancelAnimationFrame(raf)
      ro.disconnect()
      window.removeEventListener('scroll', schedule, true)
      window.removeEventListener('resize', schedule)
    }
  }, [open, side, align])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    // 延后一拍再监听，避免触发按钮自身的 click 立即把浮层关掉。
    const t = window.setTimeout(() => document.addEventListener('mousedown', onDown), 0)
    return () => {
      window.clearTimeout(t)
      document.removeEventListener('mousedown', onDown)
    }
  }, [open, onClose])

  // 菜单型浮层把焦点接管过来，并在关闭时归还触发元素。
  // 非菜单浮层（快速添加候选等）**不**动焦点，否则会破坏正在进行的输入与光标跟踪。
  useEffect(() => {
    if (!open || !menu) return
    const prev = document.activeElement as HTMLElement | null
    ref.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus()
    return () => {
      // 浮层卸载会把焦点丢回 body，此时才归还；用户若已把焦点移到别处，就不去抢。
      const active = document.activeElement
      if ((!active || active === document.body) && prev) prev.focus()
    }
  }, [open, menu])

  // Esc 交给全局仲裁栈：焦点通常停在触发按钮上（不在浮层内），
  // 无法靠容器 keydown 捕获，只能注册到栈里、由仲裁器保证"只关最上面一层"。
  useEscapeLayer(open, onClose)

  if (!open) return null
  return (
    <>
      {/*
        原位标记：portal 之后面板的 parentElement 是 body，拿不到"触发按钮所在的容器"，
        所以在这里留一个零尺寸、display:none 的锚点标记（`hidden` 不参与布局也不进无障碍树）。
      */}
      <span ref={hostRef} className="hidden" aria-hidden />
      {createPortal(
        // 落点全部由 style 精确给出（top / left / maxHeight 可能同时有值，React 会忽略
        // 值为 undefined 的键）。挂在 body 下的 fixed 元素，纵横向都是视口坐标。
        // 量测在 useLayoutEffect 里、paint 之前完成，理论上不会闪；仍用 visibility 兜底，
        // 免得极端情况下第一帧画在视口左上角。
        <div
          ref={ref}
          className={cx(
            'fixed z-40 rounded-xl border border-line bg-surface p-2 shadow-[var(--shadow-md)]',
            // 翻转时改用纯淡入：animate-rise 带 translateY(6px)，方向反了会看着别扭
            pos.side === side ? 'animate-rise' : 'animate-fade-in',
            // 仅当整屏也装不下时才收窄 + 内部滚动
            pos.maxHeight !== undefined && 'overflow-y-auto',
            className,
          )}
          style={{
            width,
            top: pos.top,
            left: pos.left,
            maxHeight: pos.maxHeight,
            visibility: pos.top === undefined ? 'hidden' : undefined,
          }}
        >
          {children}
        </div>,
        document.body,
      )}
    </>
  )
}

export function MenuItem({
  icon: Icon,
  children,
  onClick,
  danger,
  shortcut,
  disabled,
}: {
  icon?: (p: IconProps) => ReactNode
  children: ReactNode
  onClick?: () => void
  danger?: boolean
  shortcut?: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cx(
        'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.8125rem] transition-colors',
        disabled && 'cursor-not-allowed opacity-45',
        danger ? 'text-p-high hover:bg-p-high/10' : 'text-ink hover:bg-surface-2',
      )}
    >
      {/* 图标槽固定为同一宽度并居中：菜单里有些项没有图标（如「移出分组」），
          有些项用的是 8px 的 ColorDot，槽宽不一致会让文字左边界参差。 */}
      <span className="flex w-3.5 shrink-0 items-center justify-center">
        {Icon ? <Icon size={14} className="text-ink-3" /> : null}
      </span>
      <span className="flex-1 truncate">{children}</span>
      {shortcut ? <Kbd>{shortcut}</Kbd> : null}
    </button>
  )
}

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="rounded border border-line bg-surface-2 px-1.5 py-0.5 font-mono text-[0.65625rem] leading-4 text-ink-3">
      {children}
    </kbd>
  )
}

/* ---------------- 空状态 ---------------- */

export function EmptyState({
  text,
  source,
  hint,
  action,
  compact,
}: {
  text: string
  source?: string
  hint?: ReactNode
  action?: ReactNode
  compact?: boolean
}) {
  return (
    <div className={cx('flex flex-col items-center justify-center text-center', compact ? 'py-10' : 'py-20')}>
      <div className="mb-3 grid h-11 w-11 place-items-center rounded-full border border-line bg-surface-2">
        <span className="brand-serif text-[0.9375rem] text-ink-3">慎</span>
      </div>
      <p className="brand-serif max-w-[19rem] text-[0.875rem] leading-relaxed text-ink-2">{text}</p>
      {source ? <p className="mt-1 text-[0.71875rem] tracking-wide text-ink-3">{source}</p> : null}
      {hint ? <p className="mt-3 text-[0.78125rem] text-ink-3">{hint}</p> : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  )
}

/* ---------------- 其它 ---------------- */

export function Field({
  label,
  children,
  hint,
}: {
  label: string
  children: ReactNode
  hint?: string
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-[0.71875rem] text-ink-3">{hint}</span> : null}
    </label>
  )
}

export const inputClass =
  'w-full rounded-lg border border-control-line bg-surface px-2.5 py-1.5 text-[0.8125rem] text-ink outline-none transition-colors placeholder:text-ink-3 focus:border-seal'

export function ColorDot({ color, size = 8 }: { color?: string; size?: number }) {
  return (
    <span
      className="shrink-0 rounded-full"
      style={{ width: size, height: size, background: color || 'var(--ink-3)' }}
    />
  )
}

export function Chip({
  children,
  color,
  active,
  onClick,
  title,
}: {
  children: ReactNode
  color?: string
  active?: boolean
  onClick?: () => void
  title?: string
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className={cx(
        'inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[0.71875rem] transition-colors',
        active ? 'border-seal/45 bg-seal/10 text-seal' : 'border-line text-ink-2 hover:border-control-line',
        !onClick && 'cursor-default',
      )}
      style={color && !active ? { color, borderColor: `color-mix(in oklab, ${color} 38%, transparent)` } : undefined}
    >
      {children}
    </button>
  )
}

export function SectionLabel({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="group/label flex items-center justify-between px-2 pb-1 pt-4">
      <span className="text-[0.65625rem] font-semibold uppercase tracking-[0.14em] text-ink-3">{children}</span>
      {right}
    </div>
  )
}

/** 简易进度环，用于专注计时与完成率。 */
export function ProgressRing({
  value,
  size = 56,
  stroke = 4,
  color,
  children,
}: {
  value: number
  size?: number
  stroke?: number
  color?: string
  children?: ReactNode
}) {
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const clamped = Math.max(0, Math.min(1, value))
  return (
    <div className="relative inline-grid place-items-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--line)" strokeWidth={stroke} />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={color || 'var(--seal)'}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - clamped)}
          style={{ transition: 'stroke-dashoffset 0.6s cubic-bezier(0.16,1,0.3,1)' }}
        />
      </svg>
      <div className="absolute inset-0 grid place-items-center">{children}</div>
    </div>
  )
}

/** 让元素在内容变化时保持可见（用于长列表的详情联动）。 */
export function useKeepVisible<T extends HTMLElement>(dep: unknown) {
  const ref = useRef<T>(null)
  useLayoutEffect(() => {
    ref.current?.scrollIntoView({ block: 'nearest' })
  }, [dep])
  return ref
}

/** 输入框防抖提交：本地即时反馈，落库走延迟。 */
export function useDebouncedCallback<A extends unknown[]>(fn: (...args: A) => void, delay: number) {
  const timer = useRef<number | null>(null)
  const fnRef = useRef(fn)
  fnRef.current = fn
  useEffect(
    () => () => {
      if (timer.current) window.clearTimeout(timer.current)
    },
    [],
  )
  return (...args: A) => {
    if (timer.current) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => fnRef.current(...args), delay)
  }
}

/**
 * 受控的 date/time 输入，带本地草稿。
 *
 * 原生日期/时间输入是有「分段选中」态的：一旦 value 被 React 从外部改写，
 * 当前选中的那一段就被丢弃，下一次按键会另起一段 —— 表现为「输 2 再输 3，
 * 小时从 23 变成 3」。而任务字段落库是异步的（updateTask → reconcile 会重新
 * 拉取并回写 task/subtask），把 value 直接绑到远端，编辑期间的回写就会打断输入。
 *
 * 这里以本地草稿承接按键、落库走防抖，远端值只在未聚焦时同步；编辑期间一律以
 * 草稿为准，React 便不会在打字中途改写 DOM。
 */
export function DraftInput({
  type,
  value,
  onCommit,
  className,
  title,
  placeholder,
}: {
  type: 'date' | 'time'
  value: string
  onCommit: (next: string) => void
  className?: string
  title?: string
  placeholder?: string
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const focused = useRef(false)
  // 留一份最新渲染的引用，供下面的「卸载兜底提交」读到最新的草稿与回调。
  const latest = useRef({ draft: null as string | null, value, onCommit })
  latest.current = { draft, value, onCommit }
  const commit = useDebouncedCallback((next: string) => latest.current.onCommit(next), 350)
  // 远端值变化时同步草稿；正在编辑则忽略，避免打断分段输入。
  useEffect(() => {
    if (!focused.current) setDraft(null)
  }, [value])
  // 卸载兜底：防抖还没触发就把面板关掉（或任务被移出当前视图）时补一次提交，免得丢编辑。
  useEffect(
    () => () => {
      const { draft: d, value: v, onCommit: cb } = latest.current
      if (d !== null && d !== v) cb(d)
    },
    [],
  )
  return (
    <input
      type={type}
      className={className}
      title={title}
      placeholder={placeholder}
      value={draft ?? value}
      onFocus={() => {
        focused.current = true
      }}
      onBlur={() => {
        focused.current = false
        if (draft !== null && draft !== value) onCommit(draft)
      }}
      onChange={(e) => {
        setDraft(e.target.value)
        commit(e.target.value)
      }}
    />
  )
}

/** 文本域自增高。 */
export function useAutoGrow(value: string, max = 320) {
  const ref = useRef<HTMLTextAreaElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, max)}px`
  }, [value, max])
  return ref
}
