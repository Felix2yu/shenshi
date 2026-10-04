import { api } from '../api/client'
import type { WebPushSubscription } from '../types'

/**
 * Web Push 订阅。
 *
 * iOS 的两条硬前提决定了这个流程的形状，绕不开也不该绕：
 *  1. 必须 iOS 16.4 以上；
 *  2. **必须先把应用添加到主屏幕**——Safari 标签页里 `PushManager.subscribe()` 直接失败。
 *
 * 第 2 条尤其要紧：用户在设置里点了「开启通知」却什么反应都没有，八成是因为
 * 还没添加到主屏幕。所以每一步失败都要给出**具体**的下一步该做什么，
 * 而不是一句「订阅失败」把人晾在那里。
 */

/** 浏览器是否具备 Web Push 能力。 */
export function webPushSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    typeof Notification !== 'undefined'
  )
}

/** 是否运行在 iOS 上。用于给出「先添加到主屏幕」的提示。 */
export function isIOS(): boolean {
  if (typeof navigator === 'undefined') return false
  const ua = navigator.userAgent
  // iPadOS 13+ 默认报 Mac，但触摸点数能区分；只判 UA 会漏掉这一类。
  return /iP(hone|ad|od)/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

/** 是否已添加到主屏幕（standalone）。只有这时候 iOS 才允许订阅。 */
export function isStandalone(): boolean {
  if (typeof window === 'undefined') return false
  return (
    window.matchMedia?.('(display-mode: standalone)').matches === true ||
    // iOS Safari 的旧写法
    (navigator as { standalone?: boolean }).standalone === true
  )
}

/** 订阅不可用时的一句话原因。返回空串表示可以继续。 */
export function webPushBlockedReason(): string {
  if (!webPushSupported()) return '这个浏览器不支持 Web Push，请改用 Safari 16.4 以上或桌面 Chrome'
  if (isIOS() && !isStandalone()) return 'iPhone 上需要先「分享 → 添加到主屏幕」，再从主屏幕图标打开'
  if (Notification.permission === 'denied') return '通知权限已被拒绝，请在系统设置里重新允许'
  return ''
}

/**
 * 把浏览器的 PushSubscription 转成服务端要的形状。
 *
 * 取 keys 有两条路：sub.keys（已从 lib.dom 移除）与 sub.toJSON().keys。
 * 这里用 toJSON —— 它是规范里长期存在的序列化入口，旧浏览器与新浏览器都认；
 * 依赖一个已被类型定义删掉的属性，短则失效、长则变成别人的类型体操问题。
 * toJSON().keys 的键名没有类型保证，取不到时退回空串让服务端报错，
 * 比在这里抛一个 undefined 读起来的对象要好定位。
 */
async function toSubscription(sub: globalThis.PushSubscription): Promise<WebPushSubscription> {
  const keys = sub.toJSON().keys ?? {}
  return {
    endpoint: sub.endpoint,
    keys: { p256dh: keys.p256dh ?? '', auth: keys.auth ?? '' },
  }
}

/**
 * 申请权限并向服务端登记订阅。
 * **必须由用户手势触发**：Notification.requestPermission() 在 iOS 上严格如此，
 * 放在启动流程或定时器里都会失败且不报错——静默失败是最难排查的一种。
 */
export async function enableWebPush(publicKey: string): Promise<{ ok: boolean; message: string }> {
  const blocked = webPushBlockedReason()
  if (blocked) return { ok: false, message: blocked }
  if (!publicKey) return { ok: false, message: '服务端还没生成推送密钥' }

  const permission = await Notification.requestPermission()
  if (permission !== 'granted') {
    return { ok: false, message: permission === 'denied' ? '通知权限被拒绝' : '没有授予通知权限' }
  }

  const reg = await navigator.serviceWorker.ready
  // applicationServerKey 要的是 Uint8Array（base64url 解出来），
  // 直接传 base64 字符串会被 Safari 拒收且不报错。
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: base64UrlToBytes(publicKey),
  })
  await api.subscribePush(await toSubscription(sub))
  return { ok: true, message: '已开启：到点提醒会直接推到这台设备' }
}

/** 取消订阅并通知服务端。 */
export async function disableWebPush(): Promise<{ ok: boolean; message: string }> {
  try {
    const reg = await navigator.serviceWorker.ready
    const sub = await reg.pushManager.getSubscription()
    if (sub) {
      await api.unsubscribePush(sub.endpoint)
      await sub.unsubscribe()
    }
    return { ok: true, message: '已关闭本设备的服务端推送' }
  } catch (e) {
    return { ok: false, message: e instanceof Error ? e.message : '关闭失败' }
  }
}

/** 当前是否已订阅（用于设置页显示真实状态，而不是看权限标志——两者可能不一致）。 */
export async function currentWebPushSubscription(): Promise<WebPushSubscription | null> {
  if (!webPushSupported()) return null
  try {
    const reg = await navigator.serviceWorker.ready
    const sub = await reg.pushManager.getSubscription()
    // 原生对象的字段远多于服务端需要的那些，这里只取 endpoint 与 keys。
    return sub ? await toSubscription(sub) : null
  } catch {
    return null
  }
}

/**
 * base64url → Uint8Array。
 * 必须自己解：PushManager.subscribe 要的是字节序列，
 * 传字符串在 Chrome 上会报类型错误、在 Safari 上则完全没反应。
 */
function base64UrlToBytes(base64: string): ArrayBuffer {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const normalized = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(normalized)
  // 显式构造 ArrayBuffer 而不是 Uint8Array：新版 TS 的 Uint8Array 带泛型参数，
  // Uint8Array<ArrayBufferLike> 不能赋给 PushSubscriptionOptions 要求的
  // ArrayBufferView<ArrayBuffer>——这类类型体操的错误信息比问题本身还长。
  const out = new ArrayBuffer(raw.length)
  const view = new Uint8Array(out)
  for (let i = 0; i < raw.length; i += 1) view[i] = raw.charCodeAt(i)
  return out
}