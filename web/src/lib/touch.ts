import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * 触屏手势：滑动操作与长按菜单。
 *
 * 只在粗指针（手指）上启用 —— 桌面端有拖拽排序与悬停操作条，再叠一层滑动会互相打架。
 * 一次触摸只能有一种归宿：横向位移超过阈值算滑动，停在原地够久算长按，
 * 移动一点点就两者都取消，避免「手抖一下就勾掉一条任务」。
 *
 * 刻意不用 Pointer Events 之外的新 API（iOS 没有 Background Sync 那类花活），
 * 也不引入手势库：这里要的就是两个阈值判断，自己写二十行比接一个库更好维护。
 */

const SWIPE_DISTANCE = 64 // 触发滑动的横向距离（px）
const SWIPE_RATIO = 1.6 // 横向位移要显著大于纵向，否则判定为滚动
const LONG_PRESS_MS = 480
const MOVE_TOLERANCE = 8 // 超过这个位移就不再是长按

export interface RowTouchOptions {
  /** 右滑：完成或恢复。 */
  onSwipeRight?: () => void
  /** 左滑：更多操作（打开行菜单）。 */
  onSwipeLeft?: () => void
  /** 长按：同样打开行菜单，顶替 iOS 上不派发的 contextmenu。 */
  onLongPress?: () => void
  enabled: boolean
}

export interface RowTouchState {
  /** 当前横向偏移，用于行的跟手位移（正为右）。 */
  offset: number
  /** 是否已经越过阈值——越过就把背景提示点亮。 */
  armed: boolean
  handlers: {
    onTouchStart: (e: React.TouchEvent) => void
    onTouchMove: (e: React.TouchEvent) => void
    onTouchEnd: () => void
    onTouchCancel: () => void
  }
  /** 刚刚发生过手势，紧随其后的 click 应当被吃掉。 */
  consumeClick: () => boolean
}

export function useRowTouch({ onSwipeRight, onSwipeLeft, onLongPress, enabled }: RowTouchOptions): RowTouchState {
  const [offset, setOffset] = useState(0)
  const [armed, setArmed] = useState(false)
  const state = useRef({
    x0: 0,
    y0: 0,
    active: false, // 已判定为横向滑动
    longPressed: false,
    timer: 0 as number,
  })

  const clearTimer = () => {
    if (state.current.timer) {
      window.clearTimeout(state.current.timer)
      state.current.timer = 0
    }
  }

  useEffect(() => clearTimer, [])

  const reset = useCallback(() => {
    clearTimer()
    state.current.active = false
    setOffset(0)
    setArmed(false)
  }, [])

  const onTouchStart = useCallback(
    (e: React.TouchEvent) => {
      if (!enabled || e.touches.length !== 1) return
      const t = e.touches[0]
      state.current.x0 = t.clientX
      state.current.y0 = t.clientY
      state.current.longPressed = false
      clearTimer()
      if (onLongPress) {
        state.current.timer = window.setTimeout(() => {
          state.current.longPressed = true
          // 长按期间不许再触发滑动，两者互斥。
          state.current.active = false
          onLongPress()
        }, LONG_PRESS_MS)
      }
    },
    [enabled, onLongPress],
  )

  const onTouchMove = useCallback(
    (e: React.TouchEvent) => {
      if (!enabled || e.touches.length !== 1) return
      const t = e.touches[0]
      const dx = t.clientX - state.current.x0
      const dy = t.clientY - state.current.y0

      if (!state.current.active) {
        // 还没定性：位移太小什么也不做，纵向为主则交给列表滚动。
        if (Math.abs(dx) < MOVE_TOLERANCE && Math.abs(dy) < MOVE_TOLERANCE) return
        if (Math.abs(dy) * SWIPE_RATIO > Math.abs(dx)) {
          clearTimer()
          return
        }
        state.current.active = true
        // 定性为滑动后，长按作废。
        clearTimer()
        state.current.longPressed = false
      }

      // 只放行注册的那一侧：没注册右滑就拉不动，避免给出「能拖但没反应」的错觉。
      const goRight = dx > 0 && !!onSwipeRight
      const goLeft = dx < 0 && !!onSwipeLeft
      const damped = goRight || goLeft ? dx : dx * 0.2
      setOffset(damped)
      setArmed(Math.abs(damped) >= SWIPE_DISTANCE)
    },
    [enabled, onSwipeLeft, onSwipeRight],
  )

  const onTouchEnd = useCallback(() => {
    if (!enabled) return
    clearTimer()
    if (state.current.longPressed) {
      reset()
      return
    }
    if (state.current.active && Math.abs(offset) >= SWIPE_DISTANCE) {
      if (offset > 0) onSwipeRight?.()
      else onSwipeLeft?.()
    }
    reset()
  }, [enabled, offset, onSwipeLeft, onSwipeRight, reset])

  const onTouchCancel = useCallback(() => {
    if (!enabled) return
    reset()
  }, [enabled, reset])

  const consumeClick = useCallback(() => {
    if (state.current.longPressed) {
      state.current.longPressed = false
      return true
    }
    return false
  }, [])

  return {
    offset,
    armed,
    handlers: { onTouchStart, onTouchMove, onTouchEnd, onTouchCancel },
    consumeClick,
  }
}

/** 是否手指操作（决定要不要启用滑动与长按）。 */
export function coarsePointer(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(pointer: coarse)').matches
}

/* ---------------------------------------------------------------- 下拉刷新 */

const PULL_TRIGGER = 64 // 松手后真的刷新的位移
const PULL_MAX = 96 // 指示器最多下拉这么多（越拉越费劲）

export interface PullToRefresh {
  /** 当前下拉位移，用于指示器高度与「松手刷新」文案。 */
  distance: number
  refreshing: boolean
  handlers: {
    onTouchStart: (e: React.TouchEvent) => void
    onTouchMove: (e: React.TouchEvent) => void
    onTouchEnd: () => void
  }
}

/**
 * 下拉刷新。
 *
 * 装成主屏幕应用后没有地址栏，也就没有刷新按钮 —— 杀进程再开是唯一出路，
 * 这在手机上太重了。这里给列表一个下拉手势，刷的是「重新拉一次数据」。
 *
 * 只在容器已滚到顶部时接管：否则一下拉就刷新，正常滚到底再滚回来的路上会被误触发。
 */
export function usePullToRefresh(onRefresh: () => void | Promise<void>, enabled: boolean): PullToRefresh {
  const [distance, setDistance] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const start = useRef({ y: 0, armed: false })

  const onTouchStart = useCallback(
    (e: React.TouchEvent) => {
      if (!enabled || refreshing || e.touches.length !== 1) return
      const el = e.currentTarget as HTMLElement
      start.current.y = e.touches[0].clientY
      start.current.armed = el.scrollTop <= 0
    },
    [enabled, refreshing],
  )

  const onTouchMove = useCallback(
    (e: React.TouchEvent) => {
      if (!enabled || refreshing || !start.current.armed || e.touches.length !== 1) return
      const el = e.currentTarget as HTMLElement
      // 中途滚下去就不算了：那是用户改主意要滚动，不是要刷新。
      if (el.scrollTop > 0) {
        start.current.armed = false
        setDistance(0)
        return
      }
      const dy = e.touches[0].clientY - start.current.y
      if (dy <= 0) {
        setDistance(0)
        return
      }
      // 阻尼：越往下越拉不动，和系统的橡皮筋一个手感。
      setDistance(Math.min(dy * 0.5, PULL_MAX))
    },
    [enabled, refreshing],
  )

  const onTouchEnd = useCallback(async () => {
    if (!enabled || refreshing) return
    const d = distance
    start.current.armed = false
    if (d < PULL_TRIGGER) {
      setDistance(0)
      return
    }
    setRefreshing(true)
    setDistance(PULL_TRIGGER)
    try {
      await onRefresh()
    } finally {
      setRefreshing(false)
      setDistance(0)
    }
  }, [distance, enabled, onRefresh, refreshing])

  return { distance, refreshing, handlers: { onTouchStart, onTouchMove, onTouchEnd } }
}

/** 是否已达触发阈值（指示器据此切换文案）。 */
export function pullArmed(distance: number): boolean {
  return distance >= PULL_TRIGGER
}
