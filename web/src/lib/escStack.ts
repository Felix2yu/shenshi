/**
 * Esc 键「最上层优先」仲裁。
 *
 * 背景：此前 Modal 挂 window、Popover 挂 document、App 又挂 window，
 * 三者互不感知，一次 Esc 会同时被多层处理（关弹窗时连带关详情面板）。
 *
 * 机制：想要响应 Esc 的浮层把自己的 handler 压入栈；document 上只有一个仲裁器，
 * 按键时**只**执行栈顶那一层，并 `stopPropagation()` 阻止冒泡到 window，
 * 于是挂在 window 上的全局兜底（清多选 / 关详情 / 收侧栏）不会被误触发。
 *
 * 用法：容器能收到 keydown 的（如 Modal）直接在容器上处理即可；
 * 焦点不在容器内的（如 Popover 依附的触发按钮）用 `useEscapeLayer` 注册。
 */

import { useEffect, useRef } from 'react'

interface EscapeEntry {
  id: symbol
  handler: () => void
}

const stack: EscapeEntry[] = []

export function pushEscapeLayer(handler: () => void): symbol {
  const id = Symbol('escape-layer')
  stack.push({ id, handler })
  return id
}

export function popEscapeLayer(id: symbol) {
  const index = stack.findIndex((entry) => entry.id === id)
  if (index >= 0) stack.splice(index, 1)
}

/** 触发栈顶一层。返回本次按键是否被消费。 */
export function consumeEscape(): boolean {
  const top = stack[stack.length - 1]
  if (!top) return false
  top.handler()
  return true
}

export function escapeLayerDepth(): number {
  return stack.length
}

/** 组件侧用法：`active` 期间把 handler 注册为栈顶。 */
export function useEscapeLayer(active: boolean, handler: () => void) {
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => {
    if (!active) return
    const id = pushEscapeLayer(() => ref.current())
    return () => popEscapeLayer(id)
  }, [active])
}

/** 由 App 挂载一次，作为 document 上唯一的 Esc 仲裁器。 */
export function useEscapeArbiter() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return
      if (consumeEscape()) e.stopPropagation()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])
}
