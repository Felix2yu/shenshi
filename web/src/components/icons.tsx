/**
 * 内置图标集。
 *
 * 刻意不引入图标库：一是保证渲染结果可控（统一 1.7 描边、圆角端点），
 * 二是「慎始」的视觉语言需要一点自己的调性 —— 细笔画、少装饰，像铅笔在纸上画出来的。
 */

import type { ReactNode, SVGProps } from 'react'

export interface IconProps extends Omit<SVGProps<SVGSVGElement>, 'children'> {
  size?: number
}

function Base({ size = 16, children, ...rest }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      {children}
    </svg>
  )
}

export const IconInbox = (p: IconProps) => (
  <Base {...p}>
    <path d="M3 12h4l2 3h6l2-3h4" />
    <path d="M5.5 5h13l2.5 7v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-5z" />
  </Base>
)

export const IconSun = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4" />
  </Base>
)

export const IconSunrise = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 3v5M5.6 9.6 7 11M18.4 9.6 17 11M2 18h20M3 14h2M19 14h2" />
    <path d="M8 18a4 4 0 0 1 8 0" />
  </Base>
)

export const IconCalendarRange = (p: IconProps) => (
  <Base {...p}>
    <rect x="3" y="5" width="18" height="16" rx="2.5" />
    <path d="M3 10h18M8 3v4M16 3v4" />
    <path d="M7 14h3M7 17.5h6M14 14h3" />
  </Base>
)

export const IconCalendar = (p: IconProps) => (
  <Base {...p}>
    <rect x="3" y="5" width="18" height="16" rx="2.5" />
    <path d="M3 10h18M8 3v4M16 3v4" />
  </Base>
)

export const IconGrid = (p: IconProps) => (
  <Base {...p}>
    {/* 四格 8×8 满幅（3→21，墨迹 82%）在页签里显大；缩到 7×7（墨迹 78%）
        与列表/看板/表格一档，格距微增保持疏密节奏 */}
    <rect x="3.5" y="3.5" width="7" height="7" rx="2" />
    <rect x="13.5" y="3.5" width="7" height="7" rx="2" />
    <rect x="3.5" y="13.5" width="7" height="7" rx="2" />
    <rect x="13.5" y="13.5" width="7" height="7" rx="2" />
  </Base>
)

export const IconCheck = (p: IconProps) => (
  <Base {...p}>
    <path d="M4.5 12.5 9.5 17.5 19.5 6.5" />
  </Base>
)

export const IconCheckCircle = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M8 12.4 10.8 15 16 9.6" />
  </Base>
)

export const IconCircle = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="8.5" />
  </Base>
)

export const IconSearch = (p: IconProps) => (
  <Base {...p}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m16 16 4.5 4.5" />
  </Base>
)

export const IconPlus = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 5v14M5 12h14" />
  </Base>
)

export const IconMinus = (p: IconProps) => (
  <Base {...p}>
    <path d="M5 12h14" />
  </Base>
)

export const IconChevronRight = (p: IconProps) => (
  <Base {...p}>
    <path d="m9 5 7 7-7 7" />
  </Base>
)

export const IconChevronDown = (p: IconProps) => (
  <Base {...p}>
    <path d="m5 9 7 7 7-7" />
  </Base>
)

export const IconChevronLeft = (p: IconProps) => (
  <Base {...p}>
    <path d="m15 5-7 7 7 7" />
  </Base>
)

export const IconMore = (p: IconProps) => (
  <Base {...p}>
    <circle cx="5" cy="12" r="1.3" fill="currentColor" stroke="none" />
    <circle cx="12" cy="12" r="1.3" fill="currentColor" stroke="none" />
    <circle cx="19" cy="12" r="1.3" fill="currentColor" stroke="none" />
  </Base>
)

export const IconTrash = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 7h16M9 7V5h6v2M6.5 7l.9 12.1A1.9 1.9 0 0 0 9.3 21h5.4a1.9 1.9 0 0 0 1.9-1.9L17.5 7" />
    <path d="M10 11v6M14 11v6" />
  </Base>
)

export const IconPencil = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 20h4L19.5 8.5a2.1 2.1 0 0 0-3-3L5 17z" />
    <path d="m14.5 6.5 3 3" />
  </Base>
)

export const IconTag = (p: IconProps) => (
  <Base {...p}>
    <path d="M11.6 3H20v8.4a2 2 0 0 1-.6 1.4l-7 7a2 2 0 0 1-2.8 0l-6-6a2 2 0 0 1 0-2.8l7-7A2 2 0 0 1 11.6 3z" />
    <circle cx="16" cy="8" r="1.4" />
  </Base>
)

export const IconFolder = (p: IconProps) => (
  <Base {...p}>
    <path d="M3.5 7.5A2 2 0 0 1 5.5 5.5h3.6a2 2 0 0 1 1.5.7l1 1.2h7A2 2 0 0 1 20.5 9.4v8.1a2 2 0 0 1-2 2H5.5a2 2 0 0 1-2-2z" />
  </Base>
)

export const IconList = (p: IconProps) => (
  <Base {...p}>
    {/* 行距拉开到 8.5、横线延长、圆点左移：墨迹归一到 ~80×80%，与页签图标群一致 */}
    <path d="M8 3.5h12.75M8 12h12.75M8 20.5h12.75" />
    <circle cx="3.6" cy="3.5" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="3.6" cy="12" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="3.6" cy="20.5" r="1.2" fill="currentColor" stroke="none" />
  </Base>
)

export const IconFlag = (p: IconProps) => (
  <Base {...p}>
    <path d="M6 21V4" />
    <path d="M6 5h9.5l-1.2 3.2L15.5 12H6" />
  </Base>
)

export const IconBell = (p: IconProps) => (
  <Base {...p}>
    <path d="M6.5 10a5.5 5.5 0 0 1 11 0c0 3.2 1 5.2 1.7 6.2.4.6 0 1.3-.7 1.3H5.5c-.7 0-1.1-.7-.7-1.3C5.5 15.2 6.5 13.2 6.5 10z" />
    <path d="M10 20.2a2.2 2.2 0 0 0 4 0" />
  </Base>
)

export const IconRepeat = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 9V8a3 3 0 0 1 3-3h10l-2.5-2.5M20 15v1a3 3 0 0 1-3 3H7l2.5 2.5" />
    <path d="M17 5.5 19.5 8M7 18.5 4.5 16" />
  </Base>
)

export const IconClock = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 7.5V12l3 2" />
  </Base>
)

export const IconTimer = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="13.5" r="7" />
    <path d="M12 10v3.5l2.2 1.6M9.5 2.5h5M18.5 7l1.5-1.5" />
  </Base>
)

export const IconStar = (p: IconProps) => (
  <Base {...p}>
    <path d="m12 3.6 2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.8-5.2 2.8 1-5.8L3.5 9.8l5.9-.9z" />
  </Base>
)

export const IconMoon = (p: IconProps) => (
  <Base {...p}>
    <path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z" />
  </Base>
)

export const IconSettings = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </Base>
)

export const IconChart = (p: IconProps) => (
  <Base {...p}>
    {/* 原柱高 6/12/9（墨迹高仅 57%）在页签里显小；按原比例拔高并加长基线
        （墨迹 ~80×78%），保持「落地」的柱状图形特征与三柱高低节奏 */}
    <path d="M3.2 20h17.6M7 20v-9M12 20V3M17 20v-12.5" />
  </Base>
)

export const IconBook = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 5.2A1.7 1.7 0 0 1 5.7 3.5H11v17H5.7A1.7 1.7 0 0 1 4 18.8z" />
    <path d="M20 5.2a1.7 1.7 0 0 0-1.7-1.7H13v17h5.3A1.7 1.7 0 0 0 20 18.8z" />
  </Base>
)

export const IconX = (p: IconProps) => (
  <Base {...p}>
    <path d="M6 6l12 12M18 6 6 18" />
  </Base>
)

export const IconArrowRight = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 12h15M13 6l6 6-6 6" />
  </Base>
)

/** 排序方向指示：升序。 */
export const IconArrowUp = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 19V5M6 11l6-6 6 6" />
  </Base>
)

/** 排序方向指示：降序。 */
export const IconArrowDown = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 5v14M18 13l-6 6-6-6" />
  </Base>
)

export const IconColumns = (p: IconProps) => (
  <Base {...p}>
    {/* 三列加高至墨迹 ~80×80%（首列定高，次/末列按原比例 12/14），列宽 5.2→5、距边内收 */}
    <rect x="3.25" y="3.25" width="5" height="17.5" rx="1.8" />
    <rect x="9.5" y="3.25" width="5" height="12" rx="1.8" />
    <rect x="15.75" y="3.25" width="5" height="14" rx="1.8" />
  </Base>
)

export const IconFilter = (p: IconProps) => (
  <Base {...p}>
    <path d="M3.5 5.5h17l-6.5 7.6V20l-4-2v-4.9z" />
  </Base>
)

export const IconSort = (p: IconProps) => (
  <Base {...p}>
    <path d="M7 4v16M7 20l-3-3M17 20V4M17 4l3 3" />
  </Base>
)

export const IconPalette = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 3.5a8.5 8.5 0 0 0 0 17c1.2 0 1.9-.8 1.9-1.7 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.1 0-.9.8-1.7 1.7-1.7h1.5a4.4 4.4 0 0 0 4.4-4.4c0-3.8-3.8-6.9-8.5-6.9z" />
    <circle cx="7.8" cy="11.5" r="1.1" fill="currentColor" stroke="none" />
    <circle cx="11" cy="7.8" r="1.1" fill="currentColor" stroke="none" />
    <circle cx="15.6" cy="9.4" r="1.1" fill="currentColor" stroke="none" />
  </Base>
)

export const IconGrip = (p: IconProps) => (
  <Base {...p}>
    <circle cx="9" cy="6" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="15" cy="6" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="9" cy="12" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="15" cy="12" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="9" cy="18" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="15" cy="18" r="1.2" fill="currentColor" stroke="none" />
  </Base>
)

export const IconAlert = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 4.5 21 19.5H3z" />
    <path d="M12 10v4M12 17h.01" />
  </Base>
)

export const IconInfo = (p: IconProps) => (
  <Base {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 11v5M12 8h.01" />
  </Base>
)

export const IconKeyboard = (p: IconProps) => (
  <Base {...p}>
    <rect x="2.5" y="6" width="19" height="12" rx="2" />
    <path d="M6 10h.01M9.5 10h.01M13 10h.01M16.5 10h.01M8 14h8" />
  </Base>
)

export const IconSparkle = (p: IconProps) => (
  <Base {...p}>
    {/* 大星横半径 +8%、整体回中（原墨迹中心 x≈13.05 偏右）：归一 ~80×80%；
        各向异性仅 8%，描边畸变 <0.1px，无需补偿 */}
    <g transform="translate(12 12.3) scale(1.08 0.97) translate(-13.05 -12.3)">
      <path d="M12 3.5 13.6 9 19 10.6 13.6 12.2 12 17.7 10.4 12.2 5 10.6 10.4 9z" />
      <path d="M18.5 16.5l.6 2 2 .6-2 .6-.6 2-.6-2-2-.6 2-.6z" />
    </g>
  </Base>
)

export const IconMove = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 3v18M3 12h18M12 3 9.5 5.5M12 3l2.5 2.5M12 21l-2.5-2.5M12 21l2.5-2.5M3 12l2.5-2.5M3 12l2.5 2.5M21 12l-2.5-2.5M21 12l-2.5 2.5" />
  </Base>
)

export const IconNote = (p: IconProps) => (
  <Base {...p}>
    <path d="M5 4.5h14v9.5l-5 5.5H5z" />
    <path d="M19 14h-5v5.5M8.5 8.5h7M8.5 11.5h4" />
  </Base>
)

export const IconPaperclip = (p: IconProps) => (
  <Base {...p}>
    <path d="M18.5 11.5 12 18a4 4 0 0 1-5.7-5.7l7.1-7.1a2.7 2.7 0 0 1 3.8 3.8l-7.1 7.1a1.4 1.4 0 0 1-2-2l6.4-6.4" />
  </Base>
)

export const IconDownload = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 4v10M8 11l4 4 4-4" />
    <path d="M4.5 17.5v1.5a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-1.5" />
  </Base>
)

/** 模板：一张底稿加一枚复制角标。 */
export const IconTemplate = (p: IconProps) => (
  <Base {...p}>
    <rect x="3.5" y="4.5" width="13" height="13" rx="2.5" />
    <path d="M8 10.5h4M8 13.5h2.5" />
    <path d="M16.5 8.5h4v11a1.5 1.5 0 0 1-1.5 1.5H8" />
  </Base>
)

/** 预览：一只眼睛。 */
export const IconEye = (p: IconProps) => (
  <Base {...p}>
    <path d="M2.5 12S6 6.5 12 6.5 21.5 12 21.5 12 18 17.5 12 17.5 2.5 12 2.5 12z" />
    <circle cx="12" cy="12" r="2.8" />
  </Base>
)

/** 集成：两个互相咬合的端点。 */
export const IconPlug = (p: IconProps) => (
  <Base {...p}>
    <path d="M9 3v5M15 3v5" />
    <path d="M6.5 8h11v3.5a5.5 5.5 0 0 1-11 0z" />
    <path d="M12 17v4" />
  </Base>
)

export const IconSubtask = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 7h4M4 12h4M4 17h4M11 7h9M11 12h9M11 17h6" />
  </Base>
)

/** 习惯：一株破土的芽，取「积日成习」之意。 */
export const IconSeedling = (p: IconProps) => (
  <Base {...p}>
    {/* 原绘制内容仅占 viewBox ~50%（x 6.4-17.6 / y 7.2-20），在等尺寸图标中显小；
        以内容中心 (12,13.6) 放大（x 1.51 / y 1.4）到 ~80×79% 占比，描边按 y 向比例补偿以
        保持与其他图标一致的笔画视觉重量 */}
    <g transform="translate(12 13.6) scale(1.51 1.4) translate(-12 -13.6)" strokeWidth={1.7 / 1.4}>
      <path d="M12 20v-7" />
      <path d="M12 13c0-3.3-2.4-5.8-5.6-5.8C6.4 10.5 8.8 13 12 13z" />
      <path d="M12 13c0-3.3 2.4-5.8 5.6-5.8C17.6 10.5 15.2 13 12 13z" />
      <path d="M7.5 20h9" />
    </g>
  </Base>
)

/** 连续坚持：一簇火苗。 */
export const IconFlame = (p: IconProps) => (
  <Base {...p}>
    <path d="M12 3.5c3 3.2 5.5 5.8 5.5 9.3a5.5 5.5 0 1 1-11 0c0-1.9.8-3.4 2-4.8.3 1.5 1 2.4 2 2.9-.3-2.9.3-5.4 1.5-7.4z" />
  </Base>
)

/** 图钉：置顶。稍微倾斜，才像真的钉在纸上。 */
export const IconPin = (p: IconProps) => (
  <Base {...p}>
    <path d="M9.5 3h5l-.8 5.2 3.3 3.3H7l3.3-3.3z" />
    <path d="M12 11.5V21" />
  </Base>
)

/** 链接：任务的外部出处。 */
export const IconLink = (p: IconProps) => (
  <Base {...p}>
    <path d="M10 13.8a3.6 3.6 0 0 0 5.1 0l2.6-2.6a3.6 3.6 0 0 0-5.1-5.1l-1.5 1.5" />
    <path d="M14 10.2a3.6 3.6 0 0 0-5.1 0l-2.6 2.6a3.6 3.6 0 0 0 5.1 5.1l1.5-1.5" />
  </Base>
)

/** 表格：单元格网格。 */
export const IconTable = (p: IconProps) => (
  <Base {...p}>
    {/* 外框 18×15 → 17.5×17.5 居中，墨迹归一 ~80×80%；分隔线保持三等分行 / 36%-67% 列 */}
    <rect x="3.25" y="3.25" width="17.5" height="17.5" rx="2" />
    <path d="M3.25 9.1h17.5M3.25 14.9h17.5M9.5 9.1v11.65M15 9.1v11.65" />
  </Base>
)

/** 撤销：回头箭。 */
export const IconUndo = (p: IconProps) => (
  <Base {...p}>
    <path d="M4 9h9.5a5.5 5.5 0 0 1 0 11H8" />
    <path d="M7.5 5.5 4 9l3.5 3.5" />
  </Base>
)

/** 归档：带提手的收纳盒。 */
export const IconArchive = (p: IconProps) => (
  <Base {...p}>
    <rect x="3" y="4" width="18" height="4.5" rx="1.5" />
    <path d="M4.8 8.5V19a1.5 1.5 0 0 0 1.5 1.5h11.4a1.5 1.5 0 0 0 1.5-1.5V8.5" />
    <path d="M10 12.5h4" />
  </Base>
)

/** 操作历史：表盘上倒着走的指针。 */
export const IconHistory = (p: IconProps) => (
  <Base {...p}>
    <path d="M3.5 12a8.5 8.5 0 1 0 2.6-6.1" />
    <path d="M3.5 4.5V9H8" />
    <path d="M12 8v4.4l3 1.8" />
  </Base>
)

/** 复制：两张叠起来的纸。 */
export const IconCopy = (p: IconProps) => (
  <Base {...p}>
    <rect x="9" y="9" width="11.5" height="11.5" rx="2" />
    <path d="M15 6.2V5.5A1.5 1.5 0 0 0 13.5 4H5.5A1.5 1.5 0 0 0 4 5.5v8A1.5 1.5 0 0 0 5.5 15h.7" />
  </Base>
)

/** 品牌印章：朱砂方印里一个「慎」字。 */
export const SealLogo = ({ size = 34 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 40 40" aria-hidden="true" focusable="false">
    <rect x="1.5" y="1.5" width="37" height="37" rx="9" fill="var(--seal)" />
    <rect
      x="5"
      y="5"
      width="30"
      height="30"
      rx="6"
      fill="none"
      stroke="var(--seal-contrast)"
      strokeOpacity="0.42"
      strokeWidth="1"
    />
    {/* 基线放在 y=26（几何中心 20 + 0.316em）而非 20：CJK 字形的墨迹只占 em 方框的约
        0.93，按实测这是让「慎」视觉居中于方印的位置；写 28 会明显偏低。 */}
    <text
      x="20"
      y="26"
      textAnchor="middle"
      fontSize="19"
      fill="var(--seal-contrast)"
      fontFamily="Songti SC, Noto Serif SC, serif"
    >
      慎
    </text>
  </svg>
)
