import { useRef } from 'react'
import type { KeyboardEvent } from 'react'

/**
 * 中文输入法（IME）组合期守卫。
 *
 * 问题：在候选词界面按 Enter（确认候选）或 Esc（取消组合）时，
 * 浏览器仍会派发 keydown（key === 'Enter' / 'Escape'）。
 * 若输入框的 keydown 处理不区分组合状态，就会误触发提交/清空——
 * 例如快速录入框在选词时按 Esc 会把已输入的文本全部清空。
 *
 * 处理：组合期间（isComposing 或 keyCode 229）忽略 Enter/Esc。
 * 额外兼容 Safari 的怪癖：确认候选词时它会先派发 compositionend
 * 再派发 keydown，导致 isComposing 已经变回 false——因此
 * compositionend 之后的极短窗口内继续拦截这两个键。
 */
export function useIMEGuard() {
  const composingRef = useRef(false)
  const endedAtRef = useRef(0)

  const compositionProps = {
    onCompositionStart: () => {
      composingRef.current = true
    },
    onCompositionEnd: () => {
      composingRef.current = false
      endedAtRef.current = Date.now()
    },
  }

  /** 该按键是否属于输入法组合过程（应忽略，不触发提交/清空）。 */
  const isComposing = (e: KeyboardEvent<HTMLInputElement>) =>
    composingRef.current ||
    e.nativeEvent.isComposing ||
    e.keyCode === 229 ||
    (Date.now() - endedAtRef.current < 150 && (e.key === 'Enter' || e.key === 'Escape'))

  return { compositionProps, isComposing }
}
