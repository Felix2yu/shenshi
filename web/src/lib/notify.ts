/** 桌面通知与提示音。全部走浏览器原生能力，不引第三方库。 */

import { useCallback, useEffect, useState } from 'react'

/**
 * 通知的可用状态。
 * 除了浏览器自己的 Permission 三态，另加三种「查不到权限就永远 failed」的环境原因：
 * 浏览器根本没有这套 API、页面不在安全上下文、页面被跨源 iframe 嵌住。
 */
export type NotifyState = NotificationPermission | 'unsupported' | 'insecure' | 'framed'

export interface NotifyDiagnosis {
  state: NotifyState
  /** 一句话现状，直接展示给用户。 */
  label: string
  /** 能做的下一步；没有障碍时为 null。 */
  advice: string | null
  /** 是否安全上下文（https 或 localhost）。http://局域网IP 会让浏览器直接禁用通知。 */
  secure: boolean
  /** 页面是否被嵌在 iframe 里。 */
  framed: boolean
}

export function notificationSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

export function permissionState(): NotifyState {
  return diagnoseNotifications().state
}

/** 是否是安全上下文。老浏览器没有 isSecureContext 时按可用处理。 */
function isSecure(): boolean {
  return typeof window === 'undefined' || window.isSecureContext !== false
}

function isFramed(): boolean {
  try {
    return window.self !== window.top
  } catch {
    // 跨源 iframe 读 top 会抛异常，能抛就说明确实被嵌着。
    return true
  }
}

/** 针对当前浏览器给出「去哪里改回来」的路径。 */
function siteSettingsHint(): string {
  const ua = navigator.userAgent
  if (/Firefox\//.test(ua)) return '设置 → 隐私与安全 → 权限 → 通知 → 找到本站点改为「允许」'
  if (/Edg\//.test(ua)) return '地址栏左侧图标 → 网站权限 → 通知 → 允许（或 edg://settings/content/notifications）'
  if (/Chrome\//.test(ua)) return '地址栏左侧图标 → 网站设置 → 通知 → 允许（或 chrome://settings/content/notifications）'
  if (/Safari\//.test(ua)) return '系统设置 → 通知 → 找到浏览器；再在 Safari → 设置 → 网站 → 通知 → 允许'
  return '浏览器地址栏左侧的站点设置里，把「通知」改为允许'
}

/**
 * 判断桌面通知到底卡在哪一步。
 * 浏览器一旦把权限记成 denied，网页就再也不能弹授权框了 —— 只能去站点设置手动改，
 * 所以这里把「为什么是 denied」拆开讲清楚，而不是笼统丢一个 denied 给用户。
 */
export function diagnoseNotifications(): NotifyDiagnosis {
  const framed = isFramed()
  const secure = isSecure()

  if (!notificationSupported()) {
    return {
      state: 'unsupported',
      label: '此浏览器不支持桌面通知',
      advice: '提醒仍会在应用内弹出，并可按设置播放提示音。',
      secure,
      framed,
    }
  }

  const perm = Notification.permission
  if (perm === 'granted') {
    return { state: 'granted', label: '已允许', advice: null, secure, framed }
  }
  if (framed) {
    return {
      state: 'framed',
      label: '页面嵌在框架中，浏览器不允许申请通知',
      advice: '在独立标签页打开本应用后再申请，即可正常授权。',
      secure,
      framed,
    }
  }
  if (!secure) {
    return {
      state: 'insecure',
      label: '当前不是安全上下文，浏览器禁用通知',
      advice: '桌面通知只对 https 或 localhost 开放。若正通过 http://局域网IP 访问，请改用 https，或在本机用 http://localhost:8787。',
      secure,
      framed,
    }
  }
  if (perm === 'denied') {
    return {
      state: 'denied',
      label: '已被浏览器拒绝，网页无法再次弹窗申请',
      advice: `需要手动改回来：${siteSettingsHint()}。改完本页会自动刷新状态，无需重装或重启服务。`,
      secure,
      framed,
    }
  }
  return {
    state: 'default',
    label: '尚未授权',
    advice: '点「申请桌面通知权限」后，在浏览器弹窗里选择「允许」。',
    secure,
    framed,
  }
}

/**
 * 申请通知权限。返回申请后的真实状态。
 * 兼容三点：老式回调签名（旧 Safari）、非安全上下文下调用直接抛异常、
 * 以及「已经被拒绝」时不重复调用（浏览器会立刻回 denied，还会在控制台告警）。
 */
export async function requestPermission(): Promise<NotifyState> {
  const before = diagnoseNotifications()
  if (before.state === 'unsupported' || before.state === 'insecure' || before.state === 'framed') {
    return before.state
  }
  if (Notification.permission !== 'default') return Notification.permission

  try {
    const asked = await new Promise<NotificationPermission | undefined>((resolve) => {
      let settled = false
      const done = (v?: NotificationPermission) => {
        if (settled) return
        settled = true
        resolve(v)
      }
      // 新式实现返回 Promise，老式实现只认回调，这里两种都接住。
      const ret = Notification.requestPermission(done)
      if (ret && typeof (ret as Promise<NotificationPermission>).then === 'function') {
        ;(ret as Promise<NotificationPermission>).then(done).catch(() => done(undefined))
      }
    })
    return asked ?? Notification.permission
  } catch {
    return Notification.permission
  }
}

/**
 * 订阅通知状态。浏览器里改了站点权限、切回本页、或 Permissions API 上报变化时都会更新，
 * 避免「明明已经允许了，设置面板还写着 denied」这种显示与实际脱节。
 */
export function useNotifyDiagnosis(): { diag: NotifyDiagnosis; request: () => Promise<NotifyState> } {
  const [diag, setDiag] = useState<NotifyDiagnosis>(() => diagnoseNotifications())

  useEffect(() => {
    const refresh = () => setDiag(diagnoseNotifications())
    refresh()
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    let status: PermissionStatus | null = null
    const onPermChange = () => refresh()
    // Permissions API 能上报「用户在浏览器设置里改了权限」，这是刷新页面之前唯一的信号。
    if (navigator.permissions?.query) {
      navigator.permissions
        .query({ name: 'notifications' as PermissionName })
        .then((s) => {
          status = s
          s.addEventListener('change', onPermChange)
        })
        .catch(() => undefined)
    }
    return () => {
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
      status?.removeEventListener('change', onPermChange)
    }
  }, [])

  const request = useCallback(async () => {
    const r = await requestPermission()
    setDiag(diagnoseNotifications())
    return r
  }, [])

  return { diag, request }
}

/** 弹出一条通知。点击后回调（通常是聚焦窗口并跳转到对应任务）。 */
export function pushNotification(title: string, body: string, onClick?: () => void): boolean {
  if (!notificationSupported() || Notification.permission !== 'granted') return false
  try {
    const n = new Notification(title, {
      body,
      tag: `shenshi-${title}-${body}`,
      silent: false,
    })
    n.onclick = () => {
      window.focus()
      onClick?.()
      n.close()
    }
    return true
  } catch {
    return false
  }
}

let ctx: AudioContext | null = null

/** 用 WebAudio 合成一声柔和的木鱼式提示音，免去音频资源文件。 */
export function playChime(): void {
  try {
    const Ctor = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
    if (!Ctor) return
    ctx = ctx ?? new Ctor()
    if (ctx.state === 'suspended') void ctx.resume()
    const now = ctx.currentTime
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'sine'
    osc.frequency.setValueAtTime(880, now)
    osc.frequency.exponentialRampToValueAtTime(440, now + 0.32)
    gain.gain.setValueAtTime(0.0001, now)
    gain.gain.exponentialRampToValueAtTime(0.16, now + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.5)
    osc.connect(gain).connect(ctx.destination)
    osc.start(now)
    osc.stop(now + 0.55)
  } catch {
    /* 音频不可用时静默降级 */
  }
}

/** 极短的界面反馈音，用于勾选任务。 */
export function playTick(): void {
  try {
    const Ctor = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
    if (!Ctor) return
    ctx = ctx ?? new Ctor()
    if (ctx.state === 'suspended') void ctx.resume()
    const now = ctx.currentTime
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'triangle'
    osc.frequency.setValueAtTime(1320, now)
    gain.gain.setValueAtTime(0.0001, now)
    gain.gain.exponentialRampToValueAtTime(0.07, now + 0.008)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.14)
    osc.connect(gain).connect(ctx.destination)
    osc.start(now)
    osc.stop(now + 0.16)
  } catch {
    /* 忽略 */
  }
}
