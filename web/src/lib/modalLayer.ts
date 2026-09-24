/**
 * 弹层层级计数（模块级单例）。
 *
 * 目的：当有 `Modal` 打开时，把应用主内容标记为 `inert`，
 * 使背景内容既不可聚焦、也不被读屏读到（补齐 `aria-modal` 之外的一环）。
 *
 * 之所以做成全局计数而非 Context：
 * - `Modal` 的调用点分散在十几个组件里，逐个包 Provider 成本高；
 * - 嵌套弹窗（弹窗内再开弹窗）只需计数归零时才解除 inert。
 */

import { useSyncExternalStore } from 'react'

let openCount = 0
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

/** 由 `Modal` 在挂载/打开时调用。 */
export function pushModalLayer() {
  openCount += 1
  emit()
}

/** 由 `Modal` 在卸载/关闭时调用。 */
export function popModalLayer() {
  openCount = Math.max(0, openCount - 1)
  emit()
}

export function subscribeModalLayer(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function isModalLayerOpen() {
  return openCount > 0
}

/** 订阅"当前是否有弹窗打开"。App 用它给背景内容加 `inert`。 */
export function useModalLayerActive(): boolean {
  return useSyncExternalStore(subscribeModalLayer, isModalLayerOpen, () => false)
}
