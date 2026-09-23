/** 通用 UI 基元：按钮、弹层、勾选框、空状态。全部无第三方依赖。 */

import {
  useEffect,
  useLayoutEffect,
  useRef,
  type ButtonHTMLAttributes,
  type ReactNode,
} from 'react'

import { IconCheck, IconX, type IconProps } from './icons'

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
  outline: 'border border-line-strong text-ink hover:bg-surface-2',
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
        'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg transition-colors duration-150',
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
}: {
  checked: boolean
  onChange: (next: boolean) => void
  color?: string
  size?: number
  title?: string
}) {
  return (
    <button
      type="button"
      title={title ?? (checked ? '标记为未完成' : '标记为已完成')}
      aria-pressed={checked}
      onClick={(e) => {
        e.stopPropagation()
        onChange(!checked)
      }}
      className={cx(
        'group/check relative grid shrink-0 place-items-center rounded-full border transition-all duration-200',
        checked ? 'border-transparent' : 'border-line-strong hover:border-seal',
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
          checked || indeterminate ? 'border-seal bg-seal text-white' : 'border-line-strong',
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
  width = 520,
}: {
  open: boolean
  onClose: () => void
  title: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: number
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center p-4 py-[5vh]">
      <div
        className="fixed inset-0 bg-black/28 backdrop-blur-[2px] animate-fade-in"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        role="dialog"
        aria-modal="true"
        className="relative flex max-h-full w-full animate-pop flex-col overflow-hidden rounded-2xl border border-line bg-surface shadow-[var(--shadow-lg)]"
        style={{ maxWidth: `min(${width}px, calc(100vw - 2rem))` }}
      >
        <header className="flex shrink-0 items-start justify-between gap-4 border-b border-line px-5 py-4">
          <div>
            <h2 className="brand-serif text-[1.0625rem] font-semibold tracking-wide text-ink">{title}</h2>
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
    </div>
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
}: {
  open: boolean
  onClose: () => void
  children: ReactNode
  align?: 'left' | 'right' | 'center'
  side?: 'bottom' | 'top'
  width?: number
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    // 延后一拍再监听，避免触发按钮自身的 click 立即把浮层关掉。
    const t = window.setTimeout(() => document.addEventListener('mousedown', onDown), 0)
    document.addEventListener('keydown', onKey)
    return () => {
      window.clearTimeout(t)
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

  if (!open) return null
  return (
    <div
      ref={ref}
      className={cx(
        'absolute z-40 animate-rise rounded-xl border border-line bg-surface p-2 shadow-[var(--shadow-md)]',
        side === 'bottom' ? 'top-[calc(100%+6px)]' : 'bottom-[calc(100%+6px)]',
        align === 'left' && 'left-0',
        align === 'right' && 'right-0',
        align === 'center' && 'left-1/2 -translate-x-1/2',
        className,
      )}
      style={width ? { width } : undefined}
    >
      {children}
    </div>
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
      {Icon ? <Icon size={14} className="shrink-0 text-ink-3" /> : <span className="w-3.5" />}
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
  'w-full rounded-lg border border-line bg-surface px-2.5 py-1.5 text-[0.8125rem] text-ink outline-none transition-colors placeholder:text-ink-3 focus:border-seal/60'

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
        active ? 'border-seal/45 bg-seal/10 text-seal' : 'border-line text-ink-2 hover:border-line-strong',
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
