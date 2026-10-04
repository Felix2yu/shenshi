import { useEffect, useState } from 'react'

import { IconInstall, IconMore, IconPlusSquare, IconShare, IconX } from './icons'
import { Button, IconButton } from './ui'
import {
  canInstallDirectly,
  dismissInstallPrompt,
  installPlatform,
  promptInstall,
  shouldShowInstallPrompt,
  subscribeInstallPrompt,
} from '../lib/pwa'

/**
 * 主屏幕安装引导。
 *
 * iOS 上没有任何 JS 能触发安装，只有 Safari 的「分享 → 添加到主屏幕」，
 * 所以这里给的是图示步骤 + 一句「装完断网也能用」的由头；
 * Chromium 系则直接调 beforeinstallprompt 一键装。
 *
 * 出现时机：首次进入（未装成应用且近期没被关掉）时浮在右下角，
 * 关掉后一个月内不再打扰。桌面浏览器不显示——那是地址栏图标的活。
 */
export function InstallPrompt() {
  const [visible, setVisible] = useState(false)
  const [platform, setPlatform] = useState(() => installPlatform())

  // 安装在别处发生后（拿到事件 / 装好）要重新判定一次。
  useEffect(() => {
    const sync = () => {
      setPlatform(installPlatform())
      setVisible(shouldShowInstallPrompt())
    }
    // 首帧之后再看一次：beforeinstallprompt 可能比组件挂载来得更晚。
    const timer = window.setTimeout(sync, 1200)
    const off = subscribeInstallPrompt(sync)
    sync()
    return () => {
      window.clearTimeout(timer)
      off()
    }
  }, [])

  const close = () => {
    dismissInstallPrompt()
    setVisible(false)
  }

  if (!visible) return null

  const direct = canInstallDirectly()

  return (
    <div
      data-install-prompt
      // 刻意不用 role="dialog"：它不模态、不拦焦点，只是一张可关闭的提示卡。
      // 冒上 dialog 会让「弹窗」的定位（含测试脚本按 role=dialog 取第一个弹窗）认错对象。
      role="region"
      aria-label="装成应用"
      className="animate-rise kb-lift fixed bottom-[calc(6rem+env(safe-area-inset-bottom))] right-3 z-40 w-[320px] max-w-[calc(100vw-1.5rem)] rounded-xl border border-line bg-surface p-3 shadow-[var(--shadow-lg)] lg:bottom-[calc(4.5rem+env(safe-area-inset-bottom))]"
    >
      <div className="flex items-start gap-2.5">
        <div className="min-w-0 flex-1">
          <p className="text-[0.8125rem] font-medium text-ink">把慎始装到主屏幕</p>
          <p className="mt-0.5 text-[0.75rem] leading-relaxed text-ink-3">
            装成应用后是独立窗口，断网也能记，不必每次先开浏览器。
          </p>
        </div>
        <IconButton icon={IconX} label="关闭" size={14} onClick={close} className="-mr-1 -mt-1 shrink-0" />
      </div>

      {direct ? (
        <div className="mt-2.5 flex gap-2">
          <Button
            variant="primary"
            className="flex-1"
            onClick={() => {
              void promptInstall().then(() => setVisible(false))
            }}
          >
            <IconInstall size={14} />
            立即安装
          </Button>
          <Button className="flex-1" onClick={close}>
            以后再说
          </Button>
        </div>
      ) : (
        <>
          <ol className="mt-2.5 space-y-1.5 text-[0.75rem] text-ink-2">
            {platform === 'ios' ? (
              <li className="flex items-center gap-2">
                <StepIcon n={1} />
                <span className="flex items-center gap-1">
                  点底部（iPad 在右上角）的
                  <IconShare size={13} className="text-seal" />
                  分享
                </span>
              </li>
            ) : platform === 'android-chrome' ? (
              <li className="flex items-center gap-2">
                <StepIcon n={1} />
                <span className="flex items-center gap-1">
                  点右上角
                  <IconMore size={13} className="text-seal" />
                  菜单
                </span>
              </li>
            ) : (
              <li className="flex items-center gap-2">
                <StepIcon n={1} />
                <span>点地址栏右侧的安装图标</span>
              </li>
            )}
            <li className="flex items-center gap-2">
              <StepIcon n={2} />
              <span className="flex items-center gap-1">
                选
                {platform === 'ios' ? (
                  <>
                    <IconPlusSquare size={13} className="text-seal" />
                    「添加到主屏幕」
                  </>
                ) : (
                  <span className="font-medium text-ink">「安装」/「安装应用」</span>
                )}
              </span>
            </li>
            <li className="flex items-center gap-2">
              <StepIcon n={3} />
              <span>从桌面图标打开，就是独立应用</span>
            </li>
          </ol>

          {platform === 'ios' ? (
            <p className="mt-2 rounded-lg bg-surface-2 px-2 py-1.5 text-[0.6875rem] leading-relaxed text-ink-3">
              主屏幕应用与 Safari 的存储是分开的，装好后需要在应用里再输一次访问口令。
            </p>
          ) : null}

          <div className="mt-2.5 flex justify-end gap-2">
            <Button onClick={close}>知道了</Button>
          </div>
        </>
      )}
    </div>
  )
}

function StepIcon({ n }: { n: number }) {
  return (
    <span className="grid h-[18px] w-[18px] flex-none place-items-center rounded-full bg-seal text-[0.6875rem] text-seal-contrast">
      {n}
    </span>
  )
}
