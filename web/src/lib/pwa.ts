/**
 * 主屏幕应用（PWA）的运行时判定与安装引导状态。
 *
 * 三件事放在一起，是因为它们共享同一组判定：
 *   1. 现在是不是「已装成应用」在跑；
 *   2. 该不该给用户看安装引导（iOS 只能靠人手动装，没有安装事件）；
 *   3. 引导被关掉之后，什么时候可以再提示一次。
 *
 * 关于 iOS：没有 beforeinstallprompt，也没有任何 JS 能触发安装，
 * 唯一路径是 Safari 的「分享 → 添加到主屏幕」。所以 iOS 上只能给图示步骤引导，
 * 并且必须能自己判定「已经装好了」以免反复打扰。
 */

/** 标准判定：manifest 的 display 生效后，standalone / fullscreen / minimal-ui 都算独立窗口。 */
export function displayModeStandalone(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(display-mode: standalone)').matches ||
    window.matchMedia('(display-mode: fullscreen)').matches ||
    window.matchMedia('(display-mode: minimal-ui)').matches
}

/** iOS Safari 的老判定。navigator.standalone 是非标准属性，只有 iOS 上有。 */
export function iosStandalone(): boolean {
  if (typeof navigator === 'undefined') return false
  return (navigator as Navigator & { standalone?: boolean }).standalone === true
}

/** 是否已经以独立窗口（主屏幕应用 / 已安装）运行。 */
export function isStandalone(): boolean {
  return displayModeStandalone() || iosStandalone()
}

/** 是否 iOS（含 iPadOS 伪装成 Mac 的情况：靠触摸点数判定）。 */
export function isIOS(): boolean {
  if (typeof navigator === 'undefined') return false
  const ua = navigator.userAgent
  if (/iPad|iPhone|iPod/.test(ua)) return true
  // iPadOS 13+ 的 UA 与 macOS 完全一致，只有触摸点数泄露身份。
  return /Macintosh/.test(ua) && typeof navigator.maxTouchPoints === 'number' && navigator.maxTouchPoints > 1
}

/** 是否支持 Service Worker（决定要不要谈「离线可用」）。 */
export function supportsServiceWorker(): boolean {
  return typeof navigator !== 'undefined' && 'serviceWorker' in navigator
}

/** 是否支持通知。iOS 16.4 起，装到主屏幕之后才有 Web Push。 */
export function supportsNotifications(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

type Platform = 'ios' | 'android-chrome' | 'desktop-chrome' | 'other'

function platform(): Platform {
  if (isIOS()) return 'ios'
  if (typeof navigator === 'undefined') return 'other'
  const ua = navigator.userAgent
  if (/Android/.test(ua) && /Chrome|Chromium|Edg/.test(ua)) return 'android-chrome'
  if (/Chrome|Chromium|Edg/.test(ua)) return 'desktop-chrome'
  return 'other'
}

// ---------------------------------------------------------------- 安装引导

const DISMISS_KEY = 'shenshi.installPrompt.dismissedAt'
const DISMISS_AFTER_MS = 30 * 24 * 3600 * 1000 // 关掉后一个月内不再提

/** 安装引导是否已过期（关掉过且还在静默期内）。 */
export function installPromptDismissed(): boolean {
  try {
    const raw = window.localStorage.getItem(DISMISS_KEY)
    if (!raw) return false
    const at = Number(raw)
    if (!Number.isFinite(at)) return false
    return Date.now() - at < DISMISS_AFTER_MS
  } catch {
    return false
  }
}

export function dismissInstallPrompt(): void {
  try {
    window.localStorage.setItem(DISMISS_KEY, String(Date.now()))
  } catch {
    /* 隐私模式下写不进去就算了，下次打开再提一次，不影响使用 */
  }
}

/**
 * Chromium 的安装事件。必须在页面早期注册监听才有机会拿到，
 * 因此由 App 挂载时调用一次，事件对象存这里供引导卡片使用。
 */
let deferredPrompt: BeforeInstallPromptEvent | null = null

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  /** Chromium 给出的用户选择；个别实现可能缺失，调用侧兜底。 */
  readonly userChoice?: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export function captureInstallPrompt(): void {
  if (typeof window === 'undefined') return
  window.addEventListener('beforeinstallprompt', (e) => {
    // 必须阻止默认行为，否则浏览器自己的安装条会抢先弹出。
    e.preventDefault()
    deferredPrompt = e as BeforeInstallPromptEvent
    notifyListeners()
  })
  window.addEventListener('appinstalled', () => {
    deferredPrompt = null
    notifyListeners()
  })
}

const listeners = new Set<() => void>()
const notifyListeners = () => listeners.forEach((fn) => fn())

/** 订阅安装可用性的变化（拿到 beforeinstallprompt / 装好之后）。 */
export function subscribeInstallPrompt(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** 是否有「一键安装」可用（仅 Chromium 系）。 */
export function canInstallDirectly(): boolean {
  return deferredPrompt !== null
}

/** 触发一键安装。返回用户的选择；不可用则返回 null。 */
export async function promptInstall(): Promise<'accepted' | 'dismissed' | null> {
  if (!deferredPrompt) return null
  const evt = deferredPrompt
  deferredPrompt = null
  await evt.prompt()
  const choice = await evt.userChoice
  notifyListeners()
  return choice?.outcome ?? 'dismissed'
}

/** 重新打开安装引导（清掉静默标记）。设置里的「装到主屏幕」按钮用。 */
export function resetInstallPrompt(): void {
  try {
    window.localStorage.removeItem(DISMISS_KEY)
  } catch {
    /* 同上，写不进去不影响 */
  }
  notifyListeners()
}
export function shouldShowInstallPrompt(): boolean {
  if (isStandalone()) return false
  if (installPromptDismissed()) return false
  return platform() !== 'other' || canInstallDirectly()
}

/** 当前平台，引导文案据此分支。 */
export function installPlatform(): Platform {
  return platform()
}
