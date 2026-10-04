import { useEffect, useState } from 'react'

/**
 * 键盘占位高度（px），键盘收起时为 0。
 *
 * iOS 上键盘弹出时 **不缩小布局视口**：页面尺寸照旧，键盘直接盖在上面，
 * 于是 `position: fixed` 的底部标签栏、悬浮按钮、提醒卡片全被压在键盘底下 —— 用户
 * 看不见也就点不到。桌面浏览器（以及 Android 的默认行为）会真的 resize，
 * 那里这个值恒为 0，不必特殊处理。
 *
 * 判据：visualViewport 的高度比 window.innerHeight 少掉的那一段，就是键盘。
 * 给 80px 的阈值，避免地址栏收缩、页面轻微滚动这类噪声被当成键盘。
 */
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(0)

  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const update = () => {
      const hidden = Math.max(0, window.innerHeight - vv.height - vv.offsetTop)
      setInset(hidden > 80 ? Math.round(hidden) : 0)
    }
    update()
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => {
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
    }
  }, [])

  return inset
}
