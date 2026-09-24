import {
  createContext,
  useContext,
  useMemo,
  useRef,
  useState,
  useEffect,
  type ChangeEvent,
  type DragEvent,
  type ReactNode,
} from 'react'

import { EXPORT_URLS, api, type ImportMode } from '../api/client'
import { humanDay, relativeTime, todayStr } from '../lib/date'
import { describeFilter, filterFromQuery, isFilterActive } from '../lib/filter'
import { diagnoseNotifications, pushNotification, useNotifyDiagnosis, type NotifyState } from '../lib/notify'
import { useIMEGuard } from '../lib/ime'
import { ACCENTS, colorName, PALETTE } from '../lib/palette'
import { useStore } from '../store/AppStore'
import {
  ACTIVITY_LABEL,
  FONT_SCALES,
  fontScaleOf,
  type Folder,
  type List,
  type SmartKey,
  type Tag,
  type Task,
  type ThemeMode,
} from '../types'
import { SORT_MIME } from './TaskViews'
import {
  IconArchive,
  IconBell,
  IconBook,
  IconCalendar,
  IconCalendarRange,
  IconCheckCircle,
  IconChevronDown,
  IconChevronRight,
  IconCircle,
  IconFilter,
  IconFlag,
  IconFolder,
  IconGrip,
  IconGrid,
  IconHistory,
  IconInbox,
  IconList,
  IconMore,
  IconPencil,
  IconPin,
  IconPlus,
  IconSearch,
  IconSettings,
  IconStar,
  IconSun,
  IconSunrise,
  IconTag,
  IconTimer,
  IconTrash,
  IconX,
  SealLogo,
  type IconProps,
} from './icons'
import { Button, ColorDot, Field, IconButton, MenuItem, Modal, Popover, cx, inputClass } from './ui'
import { IntegrationsDialog } from './IntegrationsDialog'

type IconCmp = (p: IconProps) => ReactNode

/**
 * 跨层级拖拽时用 Context 广播「当前正被拖的是什么」：kind + id + 当前归属。
 * FolderNode 递归很深，靠 props 一层层传太笨；用 Context 让任意深度的节点都能
 * 立刻知道被拖实体的归属，从而判断「能否拖入自己」（自引用 / 环路拦截）。
 */
type DragItem = { kind: 'folder' | 'list'; id: number; parentId: number | null }
const DragCtx = createContext<{ item: DragItem | null; setItem: (i: DragItem | null) => void }>({
  item: null,
  setItem: () => {},
})

/**
 * 侧栏同层拖拽排序。只处理「平级之间调顺序」，跨层移动走 FolderNode 头部的「拖入此分组」放置区，
 * 这样既满足排序需求，又不会让一次误拖把清单换到别的分组里。
 * 返回的 props 直接展开到每一行的容器上即可。
 */
function useRowSort(
  items: { id: number; parentId: number | null }[],
  onReorder: (next: number[]) => void,
  kind: 'folder' | 'list',
  setItem: (i: DragItem | null) => void,
) {
  const ids = items.map((i) => i.id)
  const [dragId, setDragId] = useState<number | null>(null)
  const [overId, setOverId] = useState<number | null>(null)

  const propsFor = (id: number) => ({
    draggable: true,
    title: '按住拖动可调整顺序；拖到分组上可放入该分组',
    onDragStart: (e: DragEvent<HTMLDivElement>) => {
      // 分组与清单嵌套，必须阻止冒泡，否则拖清单会连带触发外层分组。
      e.stopPropagation()
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData(SORT_MIME, String(id))
      setDragId(id)
      // 广播被拖实体，供目标 FolderNode 判断能否接纳（环路 / 自引用拦截）。
      setItem({ kind, id, parentId: items.find((i) => i.id === id)?.parentId ?? null })
    },
    onDragEnd: () => {
      setDragId(null)
      setOverId(null)
      setItem(null)
    },
    onDragOver: (e: DragEvent<HTMLDivElement>) => {
      if (dragId === null || dragId === id) return
      e.preventDefault()
      e.stopPropagation()
      setOverId(id)
    },
    onDragLeave: () => setOverId((prev) => (prev === id ? null : prev)),
    onDrop: (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault()
      e.stopPropagation()
      if (dragId !== null && dragId !== id) {
        const next = [...ids]
        const from = next.indexOf(dragId)
        if (from >= 0) {
          next.splice(from, 1)
          const to = next.indexOf(id)
          if (to >= 0) {
            next.splice(to, 0, dragId)
            onReorder(next)
          }
        }
      }
      setDragId(null)
      setOverId(null)
    },
  })

  return { propsFor, overId, dragging: dragId !== null }
}

/** 拖拽中的行给一点视觉反馈，让落点可预期。 */
function dragClass(active: boolean) {
  return active ? 'ring-1 ring-seal/40 ring-inset' : ''
}

/** 触发浏览器下载。文件名由响应头的 Content-Disposition 决定。 */
function downloadExport(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

/* ---------------- 智能清单定义 ---------------- */

const SMARTS: { key: SmartKey; label: string; icon: IconCmp; countKey: string; ember?: boolean }[] = [
  { key: 'inbox', label: '收集箱', icon: IconInbox, countKey: 'inbox' },
  { key: 'today', label: '今天', icon: IconSun, countKey: 'today' },
  { key: 'tomorrow', label: '明天', icon: IconSunrise, countKey: 'tomorrow' },
  { key: 'week', label: '本周', icon: IconCalendarRange, countKey: 'week' },
  { key: 'next7', label: '最近 7 天', icon: IconCalendar, countKey: 'next7' },
  { key: 'overdue', label: '逾期', icon: IconBell, countKey: 'overdue', ember: true },
  { key: 'high', label: '高优先级', icon: IconFlag, countKey: 'high' },
  { key: 'nodate', label: '无日期', icon: IconCircle, countKey: 'nodate' },
  { key: 'starred', label: '收藏', icon: IconStar, countKey: 'starred' },
  // 「最近修改 / 最近完成」不设角标：它们不是待办量，标上数字反而误导。
  { key: 'updated', label: '最近修改', icon: IconPencil, countKey: '' },
  { key: 'recentdone', label: '最近完成', icon: IconCheckCircle, countKey: '' },
  { key: 'all', label: '全部任务', icon: IconList, countKey: 'all' },
  { key: 'done', label: '已完成', icon: IconGrid, countKey: 'done' },
]

/* ---------------- 新建 / 编辑弹窗 ---------------- */

type EntityKind = 'folder' | 'list' | 'tag'

interface EntityDraft {
  kind: EntityKind
  id?: number
  name: string
  color: string
  folderId?: number | null
  /** 新建子分组、或改写分组归属时使用 */
  parentId?: number | null
}

/**
 * 把分组树拍平成带层级信息的列表。
 * 「放入分组」候选、归档区都要看到全部层级——后端返回的是树，
 * 直接拿根级数组会把子分组漏掉，这正是分组一度只能当一级元素的原因。
 */
function flattenFolders(
  tree: Folder[],
  depth = 0,
  path: string[] = [],
): { folder: Folder; depth: number; path: string[] }[] {
  const out: { folder: Folder; depth: number; path: string[] }[] = []
  for (const f of tree) {
    out.push({ folder: f, depth, path })
    out.push(...flattenFolders(f.children ?? [], depth + 1, [...path, f.name]))
  }
  return out
}

/**
 * 分组在选择框里的显示名，写成完整路径（`生活 / 学习`）。
 *
 * 原先用的是「学习 · 在 生活 下」这种补语式说法，读起来不像中文；
 * 层级还依赖前导全角空格，缩进一旦不生效就分不出父子。改写成路径后，
 * 每个选项自身即为完整的名词短语，不依赖缩进渲染。
 */
function folderPathLabel(folder: Folder, ancestors: string[]): string {
  return [...ancestors, folder.name].join(' / ')
}

/**
 * 侧栏归档区一次拉取的任务条数。
 * 列表接口只返回本页数据、不带总数，所以「取满」只能推断为「可能还有更多」，
 * 界面上照实提示条数上限，而不是假装归档里就这么多。
 */
const ARCHIVED_TASK_LIMIT = 50

/** 递归剔除归档分组：整枝隐藏，子分组跟着父一起归档。 */
function pruneArchived(tree: Folder[]): Folder[] {
  const out: Folder[] = []
  for (const f of tree) {
    if (f.archived) continue
    out.push({ ...f, children: pruneArchived(f.children ?? []) })
  }
  return out
}

/**
 * 一个分组子树下挂的清单总数（直属 + 所有子分组里的）。
 * 侧栏分组行的角标用它——只数直属清单会把子分组里的漏掉，
 * 于是「工作」显示的数字和树上看到的清单对不上。
 */
function countListsInTree(folder: Folder, lists: List[]): number {
  const own = lists.reduce((n, l) => (l.folderId === folder.id ? n + 1 : n), 0)
  return (folder.children ?? []).reduce((n, c) => n + countListsInTree(c, lists), own)
}

/** 收集一个分组的全部后代 id。移动分组时用它挡住「把父分组塞进自己孙子」的环路。 */
function descendantIds(f: Folder): number[] {
  const out: number[] = []
  const walk = (n: Folder) => {
    for (const c of n.children ?? []) {
      out.push(c.id)
      walk(c)
    }
  }
  walk(f)
  return out
}

/**
 * 被拖实体 item 能否拖入 folder：
 * - 不能拖到自身；
 * - 分组不能拖进自己的子树（否则成环）：用 flattenFolders 在根树里找到被拖分组，再取其后代 id；
 * - 已经挂在 folder 下（parentId 相同）再「拖入」是空操作，放行高亮反而误导，故拦掉。
 */
function canNestInto(item: DragItem, folder: Folder, rootFolders: Folder[]): boolean {
  if (item.id === folder.id) return false
  if (item.kind === 'folder') {
    const dragged = flattenFolders(rootFolders).find((x) => x.folder.id === item.id)?.folder
    if (dragged && descendantIds(dragged).includes(folder.id)) return false
  }
  if (item.parentId === folder.id) return false
  return true
}

function EntityDialog({ draft, onClose }: { draft: EntityDraft | null; onClose: () => void }) {
  const {
    createFolder,
    updateFolder,
    deleteFolder,
    createList,
    updateList,
    deleteList,
    createTag,
    updateTag,
    deleteTag,
    folders,
    confirm,
  } = useStore()
  const { compositionProps, isComposing } = useIMEGuard()

  const [name, setName] = useState(draft?.name ?? '')
  const [color, setColor] = useState(draft?.color ?? PALETTE[0])
  const [folderId, setFolderId] = useState<number | null>(draft?.folderId ?? null)
  const [parentId, setParentId] = useState<number | null>(draft?.parentId ?? null)
  const [busy, setBusy] = useState(false)

  /**
   * 可选作父级的分组：拍平后包含所有层级，排除自己与自己的后代（防环路）。
   *
   * 必须排在 `if (!draft) return null` 之前 —— hooks 的调用次数不能随渲染变化。
   * 它一度位于条件返回之后，靠父组件传的 key 强制重挂载才没触发
   * "Rendered more hooks than during the previous render"，属侥幸正确。
   */
  const parentCandidates = useMemo(() => {
    if (draft?.kind !== 'folder') return []
    const all = flattenFolders(folders)
    if (draft.id === undefined) return all
    const self = all.find((x) => x.folder.id === draft.id)
    const blocked = new Set<number>([draft.id, ...(self ? descendantIds(self.folder) : [])])
    return all.filter((x) => !blocked.has(x.folder.id))
  }, [draft, folders])

  if (!draft) return null
  const isNew = draft.id === undefined
  const kindLabel = draft.kind === 'folder' ? '分组' : draft.kind === 'list' ? '清单' : '标签'

  const submit = async () => {
    const value = name.trim()
    if (!value || busy) return
    setBusy(true)
    try {
      // 只有真正落库成功才关窗：store 的更新方法吞掉异常后无法感知，
      // 这里改为看返回值，失败时留在弹窗内让用户改后重试。
      let ok = true
      if (draft.kind === 'folder') {
        if (isNew) ok = (await createFolder(value, color, parentId ?? undefined)) !== null
        else ok = await updateFolder(draft.id!, { name: value, color })
        if (ok && !isNew && draft.id !== undefined) {
          const current = folders.find((f) => f.id === draft.id)
          const wasParent = current?.parentId ?? null
          if (parentId !== wasParent) {
            ok = await updateFolder(draft.id, parentId === null ? { moveToRoot: true } : { parentId })
          }
        }
      } else if (draft.kind === 'list') {
        if (isNew) ok = (await createList(value, folderId ?? undefined, color)) !== null
        else ok = await updateList(draft.id!, { name: value, color, folderId: folderId ?? undefined, moveToRoot: folderId === null })
      } else if (isNew) {
        ok = (await createTag(value, color)) !== null
      } else {
        ok = await updateTag(draft.id!, { name: value, color })
      }
      if (ok) onClose()
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (isNew) return
    const note =
      draft.kind === 'folder'
        ? '分组内的清单与子分组会提到上一层，其中的任务不受影响。'
        : draft.kind === 'list'
          ? // 清单删除后端会连同清单内的任务一起压进撤销槽位（org.go 的 DeleteList），
            // 所以这里不能再说「不可撤销」—— 文案要与真正的行为一致，而不是更吓人。
            '清单中的所有任务会被一并删除，可在底部状态栏撤销，超过 10 分钟才彻底消失。'
          : '标签会从所有任务上移除，任务本身不受影响。'
    const ok = await confirm({
      title: `删除${kindLabel}「${draft.name}」`,
      message: note,
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return
    if (draft.kind === 'folder') await deleteFolder(draft.id!)
    else if (draft.kind === 'list') await deleteList(draft.id!)
    else await deleteTag(draft.id!)
    onClose()
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`${isNew ? '新建' : '编辑'}${kindLabel}`}
      width={420}
      footer={
        <>
          {!isNew ? (
            <Button variant="ghost" icon={IconTrash} onClick={remove} className="mr-auto text-p-high">
              删除
            </Button>
          ) : null}
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="primary" onClick={submit} disabled={busy || !name.trim()}>
            {isNew ? '创建' : '保存'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="名称">
          <input
            autoFocus
            {...compositionProps}
            className={inputClass}
            value={name}
            placeholder={draft.kind === 'folder' ? '如：工作' : draft.kind === 'list' ? '如：项目推进' : '如：深度工作'}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (isComposing(e)) return
              if (e.key === 'Enter') void submit()
            }}
          />
        </Field>

        {draft.kind === 'folder' ? (
          <Field label="放入分组">
            <select
              className={inputClass}
              value={parentId ?? ''}
              onChange={(e) => setParentId(e.target.value ? Number(e.target.value) : null)}
            >
              <option value="">不放入分组</option>
              {parentCandidates.map(({ folder: f, path }) => (
                <option key={f.id} value={f.id}>
                  {folderPathLabel(f, path)}
                </option>
              ))}
            </select>
          </Field>
        ) : null}

        {draft.kind === 'list' ? (
          <Field label="放入分组">
            <select
              className={inputClass}
              value={folderId ?? ''}
              onChange={(e) => setFolderId(e.target.value ? Number(e.target.value) : null)}
            >
              <option value="">不放入分组</option>
              {flattenFolders(folders)
                .filter((x) => !x.folder.archived)
                .map(({ folder: f, path }) => (
                  <option key={f.id} value={f.id}>
                    {folderPathLabel(f, path)}
                  </option>
                ))}
            </select>
          </Field>
        ) : null}

        <Field label="颜色">
          <div className="flex flex-wrap gap-1.5">
            {PALETTE.map((c) => (
              <button
                key={c}
                type="button"
                onClick={() => setColor(c)}
                className={cx(
                  'h-6 w-6 rounded-full border-2 transition-transform',
                  color === c ? 'scale-110 border-ink/35' : 'border-transparent hover:scale-105',
                )}
                style={{ background: c }}
                aria-label={colorName(c)}
              />
            ))}
          </div>
        </Field>
      </div>
    </Modal>
  )
}

/* ---------------- 侧边栏 ---------------- */

export function Sidebar({ onCloseRequest }: { onCloseRequest?: () => void }) {
  const { compositionProps, isComposing } = useIMEGuard()
  const {
    selection,
    select,
    selectSmart,
    view,
    counts,
    lists,
    folders,
    tags,
    settings,
    updateFolder,
    updateList,
    updateTask,
    reorderFolders,
    reorderLists,
    updateTag,
    createTag,
    deleteTag,
    confirm,
    boot,
    reminders,
    savedFilters,
    applySavedFilter,
    renameSavedFilter,
    deleteSavedFilter,
    filters,
    undo,
    undoDelete,
    dropUndo,
    activities,
    loadActivities,
    clearActivities,
    version,
  } = useStore()

  const [draft, setDraft] = useState<EntityDraft | null>(null)
  // 跨层级拖拽：当前正被拖的实体（kind/id/归属），通过 DragCtx 广播给任意深度的 FolderNode。
  const [dragItem, setDragItem] = useState<DragItem | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const [addingTag, setAddingTag] = useState(false)
  const [newTagName, setNewTagName] = useState('')
  const [renamingFilter, setRenamingFilter] = useState<number | null>(null)
  const [renameDraft, setRenameDraft] = useState('')
  const skipRenameBlur = useRef(false)
  const [appearanceOpen, setAppearanceOpen] = useState(false)
  const [archivedOpen, setArchivedOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  // 已归档任务：归档的任务不出现在任何日常视图，这里按需取一份供恢复。
  const [archivedTasks, setArchivedTasks] = useState<Task[]>([])
  useEffect(() => {
    let alive = true
    api
      .listTasks({ archived: '1', sortBy: 'updated', limit: ARCHIVED_TASK_LIMIT })
      .then((r) => {
        if (alive) setArchivedTasks(r.tasks)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [version])

  const inboxId = boot?.inboxListId ?? 0
  // 归档的分组与清单不参与主列表，只出现在折叠起来的「已归档」里。
  // 分组是树：归档与存活都要递归下去，否则子分组会跟着根级的判断一起错。
  const liveFolders = useMemo(() => pruneArchived(folders), [folders])
  const archivedFolders = useMemo(
    () => flattenFolders(folders).filter((x) => x.folder.archived).map((x) => x.folder),
    [folders],
  )
  const liveLists = useMemo(() => lists.filter((l) => !l.archived && l.id !== inboxId), [lists, inboxId])
  const starredLists = useMemo(() => liveLists.filter((l) => l.starred), [liveLists])
  const archivedLists = useMemo(() => lists.filter((l) => l.archived && l.id !== inboxId), [lists, inboxId])
  const rootLists = useMemo(() => liveLists.filter((l) => l.folderId === null), [liveLists])
  /** 顶层分组。侧栏是一棵树：分组为枝、清单为叶，两者同处一个区块。 */
  const topLevelFolders = useMemo(() => liveFolders.filter((f) => f.parentId === null), [liveFolders])

  // 分组与顶层清单各有一份排序控制器；分组内的清单在 FolderNode 内部单独维护。
  const folderSort = useRowSort(
    topLevelFolders.map((f) => ({ id: f.id, parentId: f.parentId })),
    reorderFolders,
    'folder',
    setDragItem,
  )
  const rootListSort = useRowSort(
    rootLists.map((l) => ({ id: l.id, parentId: l.folderId })),
    reorderLists,
    'list',
    setDragItem,
  )

  const smartActive = (key: SmartKey) => selection.kind === 'smart' && selection.key === key && view === 'list'

  const viewTitle = useMemo(() => {
    if (view === 'board') return '看板'
    if (view === 'table') return '表格'
    if (view === 'calendar') return '日历'
    if (view === 'quadrant') return '四象限'
    if (view === 'habits') return '习惯打卡'
    if (view === 'stats') return '统计与复盘'
    if (selection.kind === 'smart') return SMARTS.find((s) => s.key === selection.key)?.label ?? '任务'
    if (selection.kind === 'list') return lists.find((l) => l.id === selection.id)?.name ?? '清单'
    if (selection.kind === 'folder') return folders.find((f) => f.id === selection.id)?.name ?? '分组'
    if (selection.kind === 'tag') return `#${tags.find((t) => t.id === selection.id)?.name ?? ''}`
    return '搜索结果'
  }, [view, selection, lists, folders, tags])

  const confirmTagDelete = async (t: Tag) => {
    const ok = await confirm({
      title: `删除标签「${t.name}」`,
      message: '标签会从所有任务上移除，任务本身不受影响。',
      confirmText: '删除',
      danger: true,
    })
    if (ok) await deleteTag(t.id)
    setMenu(null)
  }

  return (
    <>
      {/* 窄屏（<lg）抽屉必须不透明：bg-surface/72 叠在遮罩与主区内容之上会把底层文字透出来，
          读都读不了（2026-09-24 实测）。桌面常驻侧栏仍保留 /72 的透纸质感。 */}
      <aside className="relative z-20 flex h-full w-[16.625rem] max-w-[85vw] shrink-0 flex-col border-r border-line bg-surface lg:bg-surface/72">
        {/* 品牌 */}
        <div className="flex items-center gap-2.5 px-3 pb-2 pt-3">
          <SealLogo size={32} />
          <div className="min-w-0 flex-1">
            <div className="brand-serif text-[1.0625rem] font-semibold leading-none text-ink">慎始</div>
            <div className="mt-1 truncate text-[0.65625rem] tracking-wide text-ink-3">慎始而敬终 · 行稳致远</div>
          </div>
          {onCloseRequest ? (
            <IconButton icon={IconX} label="关闭清单导航" onClick={onCloseRequest} className="lg:hidden" />
          ) : null}
          <IconButton icon={IconSettings} label="外观与设置" onClick={() => setAppearanceOpen(true)} />
        </div>

        {/* 晨省 */}
        <div className="px-3 pb-1">
          <button
            type="button"
            onClick={() => window.dispatchEvent(new CustomEvent('shenshi:morning'))}
            className="group flex w-full items-center gap-2 rounded-xl border border-seal/25 bg-seal/8 px-3 py-2 text-left transition-colors hover:bg-seal/14"
          >
            <IconSunrise size={16} className="text-seal" />
            <span className="min-w-0 flex-1">
              <span className="brand-serif block text-[0.8125rem] font-medium text-ink">晨省 · 规划今日</span>
              <span className="block truncate text-[0.65625rem] text-ink-3">凡事豫则立，不豫则废</span>
            </span>
            <IconChevronRight size={14} className="shrink-0 text-seal/60 transition-transform group-hover:translate-x-0.5" />
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 pb-3">
          <DragCtx.Provider value={{ item: dragItem, setItem: setDragItem }}>
          {/* 智能清单 */}
          <div className="pb-1 pt-3">
            {SMARTS.map((s) => {
              const n = counts[s.countKey] ?? 0
              if (s.key === 'overdue' && n === 0 && !smartActive(s.key)) return null
              const active = smartActive(s.key)
              return (
                <button
                  key={s.key}
                  type="button"
                  onClick={() => selectSmart(s.key)}
                  className={cx(
                    'group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left text-[0.8125rem] transition-colors',
                    active ? 'bg-seal/10 font-medium text-seal' : 'text-ink-2 hover:bg-surface-2 hover:text-ink',
                  )}
                >
                  <s.icon size={15} className={cx('shrink-0', active ? 'text-seal' : 'text-ink-3')} />
                  <span className="flex-1 truncate">{s.label}</span>
                  {s.key === 'today' ? <span className="text-[0.65625rem] text-ink-3">{humanDay(todayStr())}</span> : null}
                  {n > 0 ? (
                    <span
                      className={cx(
                        'min-w-4 rounded-full px-1.5 text-center text-[0.65625rem] tabular-nums',
                        s.ember ? 'bg-p-high/12 text-p-high' : active ? 'bg-seal/15 text-seal' : 'text-ink-3',
                      )}
                    >
                      {n}
                    </span>
                  ) : null}
                </button>
              )
            })}
          </div>

          {/* 这里不放视图入口：侧栏只负责「看哪些任务」（范围），
              「怎么读这批任务」（视图）统一交给工具栏右侧的视图页签。
              此前这里另有一份 6 项竖排入口，与工具栏完全重复（同一 setView），
              且同一功能的图标与文案两处还对不上，故于 2026-09-24 移除。 */}

          {/* 收藏的清单：同一张清单在主树里也会出现（收藏不是「移走」），
              所以区块名里点明这是快捷入口，免得被当成重复渲染的 bug。 */}
          {starredLists.length > 0 ? (
            <div className="mt-1 border-t border-line pt-1">
              <div className="px-2 pb-1 pt-3 text-[0.65625rem] font-semibold uppercase tracking-[0.14em] text-ink-3">
                收藏的清单 · 快捷方式
              </div>
              {starredLists.map((l) => (
                <ListRow
                  key={l.id}
                  list={l}
                  active={selection.kind === 'list' && selection.id === l.id && view === 'list'}
                  onSelect={() => select({ kind: 'list', id: l.id })}
                  onEdit={() => setDraft({ kind: 'list', id: l.id, name: l.name, color: l.color, folderId: l.folderId })}
                  menu={menu}
                  setMenu={setMenu}
                  folders={liveFolders}
                />
              ))}
            </div>
          ) : null}

          {/* 保存的筛选：套用即把工具栏那套条件换掉 */}
          {savedFilters.length > 0 ? (
            <div className="mt-1 border-t border-line pt-1">
              <div className="flex items-center gap-1 px-2 pb-1 pt-3">
                <span className="flex-1 text-[0.65625rem] font-semibold uppercase tracking-[0.14em] text-ink-3">
                  保存的筛选
                </span>
                {isFilterActive(filters) ? (
                  <span className="text-[0.625rem] text-ink-3" title="当前工具栏已有筛选条件">
                    筛选中
                  </span>
                ) : null}
              </div>
              {savedFilters.map((f) =>
                renamingFilter === f.id ? (
                  <div key={f.id} className="flex items-center gap-1 px-2 py-1">
                    <input
                      autoFocus
                      {...compositionProps}
                      className="min-w-0 flex-1 rounded-md border border-seal/50 bg-surface px-2 py-1 text-[0.78125rem] outline-none focus:border-seal/60"
                      value={renameDraft}
                      onChange={(e) => setRenameDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (isComposing(e)) return
                        if (e.key === 'Enter') e.currentTarget.blur()
                        if (e.key === 'Escape') {
                          skipRenameBlur.current = true
                          e.currentTarget.blur()
                        }
                      }}
                      onBlur={() => {
                        if (skipRenameBlur.current) {
                          skipRenameBlur.current = false
                          setRenamingFilter(null)
                          return
                        }
                        const v = renameDraft.trim()
                        if (v && v !== f.name) void renameSavedFilter(f.id, v)
                        setRenamingFilter(null)
                      }}
                    />
                  </div>
                ) : (
                  <div key={f.id} className="group/filter flex items-center gap-1 rounded-lg pr-1.5 hover:bg-surface-2">
                    <button
                      type="button"
                      data-saved-filter={f.id}
                      onClick={() => applySavedFilter(f)}
                      className="flex min-w-0 flex-1 items-start gap-2.5 py-[6px] pl-2.5 text-left"
                    >
                      <IconFilter size={13} className="mt-[3px] shrink-0 text-ink-3" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[0.8125rem] text-ink-2">{f.name}</span>
                        <span className="block truncate text-[0.65625rem] text-ink-3">
                          {describeFilter(filterFromQuery(f.query), tags) || '无条件'}
                        </span>
                      </span>
                    </button>
                    <button
                      type="button"
                      title="重命名这条筛选"
                      onClick={() => {
                        setRenamingFilter(f.id)
                        setRenameDraft(f.name)
                      }}
                      className="rounded p-0.5 text-ink-3 opacity-0 transition-opacity hover:text-ink group-hover/filter:opacity-100"
                    >
                      <IconPencil size={12} />
                    </button>
                    <button
                      type="button"
                      title="删除这条筛选"
                      onClick={async () => {
                        const ok = await confirm({
                          title: `删除筛选「${f.name}」`,
                          message: '删除后不可恢复，筛选条件本身不会影响任务。',
                          confirmText: '删除',
                          danger: true,
                        })
                        if (ok) void deleteSavedFilter(f.id)
                      }}
                      className="rounded p-0.5 text-ink-3 opacity-0 transition-opacity hover:text-p-high group-hover/filter:opacity-100"
                    >
                      <IconX size={12} />
                    </button>
                  </div>
                ),
              )}
            </div>
          ) : null}

          {/* 清单树：分组是文件夹（可嵌套）、清单是装任务的叶子，两者同处一棵树。
              旧的「分组 / 清单」两个平级区块按「有无上级分组」把同一类东西劈成两半，
              于是分组下的清单在「清单」区里根本找不到——这是层级看起来错乱的根源。 */}
          <div className="mt-1 border-t border-line pt-1">
            <SideHeader
              label="清单"
              onAdd={() => setDraft({ kind: 'list', name: '', color: PALETTE[0], folderId: null })}
              addLabel="新建清单"
              onAddSecondary={() => setDraft({ kind: 'folder', name: '', color: PALETTE[2], parentId: null })}
              addLabelSecondary="新建分组"
            />
            {topLevelFolders.map((f) => (
              <div
                key={`folder-${f.id}`}
                {...folderSort.propsFor(f.id)}
                className={cx('rounded-lg', dragClass(folderSort.overId === f.id))}
              >
                <FolderNode
                  folder={f}
                  allLiveLists={liveLists}
                  rootFolders={topLevelFolders}
                  selection={selection}
                  view={view}
                  menu={menu}
                  setMenu={setMenu}
                  onSelectFolder={(id) => select({ kind: 'folder', id })}
                  onSelectList={(id) => select({ kind: 'list', id })}
                  onToggleCollapse={(id, collapsed) => void updateFolder(id, { collapsed })}
                  onEditFolder={(target) =>
                    setDraft({
                      kind: 'folder',
                      id: target.id,
                      name: target.name,
                      color: target.color,
                      parentId: target.parentId,
                    })
                  }
                  onEditList={(l) =>
                    setDraft({ kind: 'list', id: l.id, name: l.name, color: l.color, folderId: l.folderId })
                  }
                  onAddList={(target) => setDraft({ kind: 'list', name: '', color: target.color, folderId: target.id })}
                  onAddSubfolder={(target) =>
                    setDraft({ kind: 'folder', name: '', color: target.color, parentId: target.id })
                  }
                  onToggleArchive={(id, archived) => void updateFolder(id, { archived })}
                  onReorderLists={reorderLists}
                  onReorderChildFolders={reorderFolders}
                />
              </div>
            ))}

            {rootLists.map((l) => (
              <div
                key={`list-${l.id}`}
                {...rootListSort.propsFor(l.id)}
                className={cx('rounded-lg', dragClass(rootListSort.overId === l.id))}
              >
                <ListRow
                  list={l}
                  active={selection.kind === 'list' && selection.id === l.id && view === 'list'}
                  onSelect={() => select({ kind: 'list', id: l.id })}
                  onEdit={() => setDraft({ kind: 'list', id: l.id, name: l.name, color: l.color, folderId: l.folderId })}
                  menu={menu}
                  setMenu={setMenu}
                  folders={liveFolders}
                />
              </div>
            ))}
            {rootLists.length === 0 && topLevelFolders.length === 0 ? (
              <p className="px-2.5 py-1 text-[0.71875rem] text-ink-3">还没有清单，点标题右侧的 + 开始。</p>
            ) : null}
          </div>

          {/* 标签 */}
          <div className="mt-1 border-t border-line pt-1">
            <SideHeader label="标签" onAdd={() => setAddingTag(true)} addLabel="新建标签" />
            {addingTag ? (
              <form
                className="flex items-center gap-1 px-2 py-1"
                onSubmit={async (e) => {
                  e.preventDefault()
                  const name = newTagName.trim()
                  if (!name) {
                    setAddingTag(false)
                    return
                  }
                  // 创建失败（重名等）返回 null：保持输入不收起，让用户改后再试。
                  const created = await createTag(name)
                  if (created) {
                    setNewTagName('')
                    setAddingTag(false)
                  }
                }}
              >
                <input
                  autoFocus
                  {...compositionProps}
                  className="min-w-0 flex-1 rounded-md border border-line bg-surface px-2 py-1 text-[0.78125rem] outline-none focus:border-seal/60"
                  placeholder="标签名"
                  value={newTagName}
                  onChange={(e) => setNewTagName(e.target.value)}
                  onKeyDown={(e) => {
                    if (isComposing(e)) e.preventDefault()
                  }}
                  onBlur={() => !newTagName.trim() && setAddingTag(false)}
                />
                <IconButton icon={IconX} label="取消" size={13} onClick={() => setAddingTag(false)} />
              </form>
            ) : null}
            {tags.map((t) => {
              const active = selection.kind === 'tag' && selection.id === t.id && view === 'list'
              const key = `tag-${t.id}`
              return (
                <div key={t.id} className="group/tag relative">
                  <div
                    className={cx(
                      'flex items-center gap-1 rounded-lg pr-1.5 transition-colors hover:bg-surface-2',
                      active && 'bg-seal/10',
                    )}
                  >
                    <button
                      type="button"
                      onClick={() => select({ kind: 'tag', id: t.id })}
                      className={cx(
                        'flex min-w-0 flex-1 items-center gap-2.5 py-[7px] pl-2.5 text-left text-[0.8125rem]',
                        active ? 'font-medium text-seal' : 'text-ink-2 hover:text-ink',
                      )}
                    >
                      <IconTag size={14} className="shrink-0" style={{ color: t.color }} />
                      <span className="truncate">{t.name}</span>
                      {t.taskCount ? <span className="text-[0.65625rem] text-ink-3 tabular-nums">{t.taskCount}</span> : null}
                    </button>
                    <IconButton
                      icon={IconMore}
                      label={`「${t.name}」的更多操作`}
                      size={13}
                      aria-expanded={menu === key}
                      className="opacity-0 transition-opacity group-hover/tag:opacity-100"
                      onClick={(e) => {
                        e.stopPropagation()
                        setMenu(menu === key ? null : key)
                      }}
                    />
                  </div>
                  <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={150}>
                    <div className="flex flex-wrap gap-1 px-1.5 py-1.5">
                      {PALETTE.map((c) => (
                        <button
                          key={c}
                          type="button"
                          aria-label={colorName(c)}
                          onClick={() => {
                            void updateTag(t.id, { color: c })
                            setMenu(null)
                          }}
                          className={cx(
                            'h-5 w-5 rounded-full border-2 transition-transform hover:scale-110',
                            t.color === c ? 'border-ink/35' : 'border-transparent',
                          )}
                          style={{ background: c }}
                        />
                      ))}
                    </div>
                    <div className="border-t border-line pt-1">
                      <MenuItem
                        icon={IconPencil}
                        onClick={() => {
                          setDraft({ kind: 'tag', id: t.id, name: t.name, color: t.color })
                          setMenu(null)
                        }}
                      >
                        重命名
                      </MenuItem>
                      <MenuItem icon={IconTrash} danger onClick={() => void confirmTagDelete(t)}>
                        删除
                      </MenuItem>
                    </div>
                  </Popover>
                </div>
              )
            })}
            {tags.length === 0 && !addingTag ? (
              <p className="px-2.5 py-1 text-[0.71875rem] leading-relaxed text-ink-3">
                在任务标题里输入 <span className="text-seal">#标签</span> 即可自动创建。
              </p>
            ) : null}
          </div>

          {/* 已归档：折叠收纳，默认收起，避免淹没主列表 */}
          {archivedFolders.length > 0 || archivedLists.length > 0 || archivedTasks.length > 0 ? (
            <div className="mt-1 border-t border-line pt-1">
              <button
                type="button"
                aria-expanded={archivedOpen}
                onClick={() => setArchivedOpen((v) => !v)}
                className="flex w-full items-center gap-2 px-2.5 py-1 text-[0.65625rem] font-semibold uppercase tracking-[0.14em] text-ink-3 transition-colors hover:text-ink-2"
              >
                <IconChevronRight size={12} className={cx('transition-transform', archivedOpen && 'rotate-90')} />
                已归档
                <span className="text-[0.625rem] font-normal normal-case tracking-normal">
                  {archivedFolders.length + archivedLists.length + archivedTasks.length}
                </span>
              </button>
              {archivedOpen ? (
                <div className="pb-1">
                  {archivedFolders.map((f) => (
                    <button
                      key={f.id}
                      type="button"
                      onClick={() => void updateFolder(f.id, { archived: false })}
                      className="group/arc flex w-full items-center gap-2 rounded-lg px-2.5 py-[6px] text-left text-[0.78125rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink"
                    >
                      <IconArchive size={12} className="shrink-0" />
                      <span className="flex-1 truncate">{f.name}</span>
                      <span className="text-[0.625rem] opacity-0 transition-opacity group-hover/arc:opacity-100">恢复</span>
                    </button>
                  ))}
                  {archivedLists.map((l) => (
                    <button
                      key={l.id}
                      type="button"
                      onClick={() => void updateList(l.id, { archived: false })}
                      className="group/arc flex w-full items-center gap-2 rounded-lg px-2.5 py-[6px] text-left text-[0.78125rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink"
                    >
                      <IconArchive size={12} className="shrink-0" />
                      <span className="flex-1 truncate">{l.name}</span>
                      <span className="text-[0.625rem] opacity-0 transition-opacity group-hover/arc:opacity-100">恢复</span>
                    </button>
                  ))}
                  {archivedTasks.map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => void updateTask(t.id, { archived: false })}
                      className="group/arc flex w-full items-center gap-2 rounded-lg px-2.5 py-[6px] text-left text-[0.78125rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink"
                    >
                      <IconArchive size={12} className="shrink-0" />
                      <span className="flex-1 truncate">{t.title}</span>
                      <span className="text-[0.625rem] opacity-0 transition-opacity group-hover/arc:opacity-100">恢复</span>
                    </button>
                  ))}
                  {archivedTasks.length >= ARCHIVED_TASK_LIMIT ? (
                    <p className="px-2.5 py-[6px] text-[0.6875rem] leading-relaxed text-ink-3">
                      仅显示最近 {ARCHIVED_TASK_LIMIT} 条，恢复其中的任务后会自动往下补更早的。
                    </p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
          </DragCtx.Provider>
        </nav>

        {/* 底部 */}
        <div className="border-t border-line px-3 py-2">
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => window.dispatchEvent(new CustomEvent('shenshi:review'))}
              className="flex flex-1 items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink-2 transition-colors hover:bg-surface-2 hover:text-ink"
            >
              <IconBook size={14} className="shrink-0 text-ink-3" />
              <span className="whitespace-nowrap">日省 · 今日复盘</span>
            </button>
            <IconButton
              icon={IconHistory}
              label="操作历史"
              onClick={() => {
                void loadActivities()
                setHistoryOpen(true)
              }}
            />
            <IconButton
              icon={IconTimer}
              label="专注计时"
              onClick={() => window.dispatchEvent(new CustomEvent('shenshi:focus'))}
            />
          </div>
          <div className="mt-1 flex items-center gap-1.5 px-2.5 text-[0.65625rem] text-ink-3">
            <IconBell size={12} className="shrink-0" />
            <span className="truncate">{reminders.length > 0 ? `${reminders.length} 条提醒待处理` : '提醒已就绪'}</span>
            <span className="ml-auto flex items-center gap-1.5">
              {undo?.available ? (
                <button
                  type="button"
                  onClick={() => void undoDelete()}
                  title={`撤销：${undo.label ?? '删除'}`}
                  className="shrink-0 rounded px-1 py-0.5 font-medium text-seal transition-colors hover:bg-seal/10"
                >
                  撤销
                </button>
              ) : null}
              <span className="shrink-0 tabular-nums">{viewTitle}</span>
            </span>
          </div>
        </div>
      </aside>

      <EntityDialog key={draft ? `${draft.kind}-${draft.id ?? 'new'}` : 'none'} draft={draft} onClose={() => setDraft(null)} />
      <AppearanceDialog open={appearanceOpen} onClose={() => setAppearanceOpen(false)} />
      <HistoryDialog open={historyOpen} onClose={() => setHistoryOpen(false)} />
    </>
  )
}

/* ---------------- 小组件 ---------------- */

function SideHeader({
  label,
  onAdd,
  addLabel,
  onAddSecondary,
  addLabelSecondary,
}: {
  label: string
  onAdd: () => void
  addLabel: string
  /** 第二个新建入口。分组与清单同处一棵树，标题行要能分别新建两者。 */
  onAddSecondary?: () => void
  addLabelSecondary?: string
}) {
  return (
    <div className="flex items-center justify-between px-2 pb-1 pt-3">
      <span className="text-[0.65625rem] font-semibold uppercase tracking-[0.14em] text-ink-3">{label}</span>
      <div className="flex items-center gap-0.5">
        {onAddSecondary ? (
          <IconButton icon={IconFolder} label={addLabelSecondary ?? '新建'} size={13} onClick={onAddSecondary} />
        ) : null}
        <IconButton icon={IconPlus} label={addLabel} size={13} onClick={onAdd} />
      </div>
    </div>
  )
}

/**
 * 分组节点。它会递归渲染 folder.children，所以分组能挂到任意层级。
 * 子分组与清单共用「先子分组、后清单」的顺序：容器在上，装任务的叶子在下。
 */
function FolderNode({
  folder,
  allLiveLists,
  rootFolders,
  selection,
  view,
  menu,
  setMenu,
  onSelectFolder,
  onSelectList,
  onToggleCollapse,
  onEditFolder,
  onEditList,
  onAddList,
  onAddSubfolder,
  onToggleArchive,
  onReorderLists,
  onReorderChildFolders,
}: {
  folder: Folder
  /** 未归档且不含收集箱的清单全集；每个节点自己按 folderId 取直属清单。 */
  allLiveLists: List[]
  /** 根级分组树（未归档）。环路校验要在整棵树里找被拖分组的后代，单看本节点不够。 */
  rootFolders: Folder[]
  selection: ReturnType<typeof useStore>['selection']
  view: string
  menu: string | null
  setMenu: (k: string | null) => void
  onSelectFolder: (id: number) => void
  onSelectList: (id: number) => void
  onToggleCollapse: (id: number, collapsed: boolean) => void
  onEditFolder: (f: Folder) => void
  onEditList: (l: List) => void
  onAddList: (f: Folder) => void
  onAddSubfolder: (f: Folder) => void
  onToggleArchive: (id: number, archived: boolean) => void
  onReorderLists: (ids: number[]) => void
  /** 同级子分组重排。后端按传入 id 子集改写 sort_order，所以只传同级即可。 */
  onReorderChildFolders: (ids: number[]) => void
}) {
  const key = `folder-${folder.id}`
  const active = selection.kind === 'folder' && selection.id === folder.id && view === 'list'
  const lists = allLiveLists.filter((l) => l.folderId === folder.id)
  const childFolders = folder.children ?? []
  /** 角标取子树总数：只数直属清单会把子分组里的漏掉，与树上看到的数量对不上。 */
  const subtreeListCount = countListsInTree(folder, allLiveLists)
  // 跨层级拖拽：读全局被拖实体；本节点作为「拖入此分组」的放置目标。
  const { item: dragItem, setItem } = useContext(DragCtx)
  const { updateFolder, updateList } = useStore()
  const [dropInto, setDropInto] = useState(false)
  const listSort = useRowSort(
    lists.map((l) => ({ id: l.id, parentId: l.folderId })),
    onReorderLists,
    'list',
    setItem,
  )
  const childSort = useRowSort(
    childFolders.map((c) => ({ id: c.id, parentId: c.parentId })),
    onReorderChildFolders,
    'folder',
    setItem,
  )

  // 拖拽悬停判定：指针落在行高中间 40%（30%~70%）为「拖入此分组」，上下边缘留给
  // 同层排序（事件继续冒泡到外层 wrapper 的 onDragOver/onDrop）。这样两个语义互不挤占。
  const inNestZone = (e: DragEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const y = (e.clientY - rect.top) / Math.max(rect.height, 1)
    return y > 0.3 && y < 0.7
  }
  const onFolderDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!dragItem) return
    if (!inNestZone(e)) {
      if (dropInto) setDropInto(false)
      return // 边缘区：放行给外层做同层排序
    }
    if (!canNestInto(dragItem, folder, rootFolders)) return
    e.preventDefault()
    e.stopPropagation()
    if (!dropInto) setDropInto(true)
  }
  const onFolderDrop = (e: DragEvent<HTMLDivElement>) => {
    const item = dragItem
    if (dropInto) setDropInto(false)
    if (!item) return
    if (!inNestZone(e)) return // 边缘区：放行给外层做同层排序
    if (!canNestInto(item, folder, rootFolders)) return
    e.preventDefault()
    e.stopPropagation()
    setItem(null)
    if (item.kind === 'folder') void updateFolder(item.id, { parentId: folder.id })
    else void updateList(item.id, { folderId: folder.id })
  }
  const draggingSelf = dragItem?.kind === 'folder' && dragItem.id === folder.id
  return (
    <div>
      <div
        className={cx(
          'group/folder flex items-center gap-0.5 rounded-lg pl-0.5 pr-1.5 transition-colors hover:bg-surface-2',
          active && 'bg-seal/10',
          dropInto && 'bg-seal/15 ring-1 ring-seal',
          draggingSelf && 'opacity-50',
        )}
        onDragOver={onFolderDragOver}
        onDragLeave={() => setDropInto(false)}
        onDrop={onFolderDrop}
      >
        <button
          type="button"
          onClick={() => onToggleCollapse(folder.id, !folder.collapsed)}
          aria-label={folder.collapsed ? '展开分组' : '折叠分组'}
          className="grid h-6 w-5 shrink-0 place-items-center rounded text-ink-3 hover:text-ink"
        >
          {folder.collapsed ? <IconChevronRight size={12} /> : <IconChevronDown size={12} />}
        </button>
        <button
          type="button"
          onClick={() => onSelectFolder(folder.id)}
          className={cx(
            'flex min-w-0 flex-1 items-center gap-2 py-[7px] text-left text-[0.8125rem]',
            active ? 'font-medium text-seal' : 'text-ink',
          )}
        >
          {/* 分组用文件夹图标（描边取分组配色 + 淡填充），清单用实心圆点——
              两者同处一棵树后，靠形状而非颜色才能一眼分清容器与叶子。 */}
          <IconFolder
            size={14}
            className="shrink-0"
            style={{ color: folder.color || 'var(--ink-3)' }}
            fill="currentColor"
            fillOpacity={0.18}
          />
          <span className="truncate">{folder.name}</span>
          {folder.archived ? <IconArchive size={12} className="shrink-0 text-ink-3" aria-label="已归档" /> : null}
          {/* 角标带单位：分组数的是子树清单、清单数的是任务，量纲不同，不带单位会看混。 */}
          {subtreeListCount > 0 ? (
            <span className="text-[0.65625rem] text-ink-3 tabular-nums">{subtreeListCount} 清单</span>
          ) : null}
        </button>
        <span className="opacity-0 transition-opacity group-hover/folder:opacity-100">
          <IconGrip size={12} className="text-ink-3/50" />
        </span>
        <IconButton
          icon={IconMore}
          label={`「${folder.name}」的更多操作`}
          size={13}
          aria-expanded={menu === key}
          className="opacity-0 transition-opacity group-hover/folder:opacity-100"
          onClick={(e) => {
            e.stopPropagation()
            setMenu(menu === key ? null : key)
          }}
        />
        <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={186}>
          <MenuItem
            icon={IconPlus}
            onClick={() => {
              onAddList(folder)
              setMenu(null)
            }}
          >
            新建清单
          </MenuItem>
          <MenuItem
            icon={IconFolder}
            onClick={() => {
              onAddSubfolder(folder)
              setMenu(null)
            }}
          >
            新建子分组
          </MenuItem>
          {/* 打开的是编辑弹窗（名称、配色、放入分组、删除都在里面），故用铅笔而非
              垃圾桶 + 危险色；也正因为它已覆盖「改名与配色」，不再单列一个同义项。 */}
          <MenuItem
            icon={IconPencil}
            onClick={() => {
              onEditFolder(folder)
              setMenu(null)
            }}
          >
            编辑分组…
          </MenuItem>
          <MenuItem
            icon={folder.collapsed ? IconChevronDown : IconChevronRight}
            onClick={() => {
              onToggleCollapse(folder.id, !folder.collapsed)
              setMenu(null)
            }}
          >
            {folder.collapsed ? '展开分组' : '折叠分组'}
          </MenuItem>
          <MenuItem
            icon={folder.archived ? IconFolder : IconArchive}
            onClick={() => {
              onToggleArchive(folder.id, !folder.archived)
              setMenu(null)
            }}
          >
            {folder.archived ? '取消归档' : '归档'}
          </MenuItem>
        </Popover>
      </div>

      {!folder.collapsed ? (
        <div className="ml-[15px] border-l border-line pl-1.5">
          {childFolders.map((cf) => (
            <div
              key={cf.id}
              {...childSort.propsFor(cf.id)}
              className={cx('rounded-lg', dragClass(childSort.overId === cf.id))}
            >
              <FolderNode
                folder={cf}
                allLiveLists={allLiveLists}
                rootFolders={rootFolders}
                selection={selection}
                view={view}
                menu={menu}
                setMenu={setMenu}
                onSelectFolder={onSelectFolder}
                onSelectList={onSelectList}
                onToggleCollapse={onToggleCollapse}
                onEditFolder={onEditFolder}
                onEditList={onEditList}
                onAddList={onAddList}
                onAddSubfolder={onAddSubfolder}
                onToggleArchive={onToggleArchive}
                onReorderLists={onReorderLists}
                onReorderChildFolders={onReorderChildFolders}
              />
            </div>
          ))}
          {lists.map((l) => (
            <div
              key={l.id}
              {...listSort.propsFor(l.id)}
              className={cx('rounded-lg', dragClass(listSort.overId === l.id))}
            >
              <ListRow
                list={l}
                active={selection.kind === 'list' && selection.id === l.id && view === 'list'}
                onSelect={() => onSelectList(l.id)}
                onEdit={() => onEditList(l)}
                menu={menu}
                setMenu={setMenu}
                // 分组内的清单同样需要「移动到分组」候选；漏传会让整段菜单消失
                folders={rootFolders}
              />
            </div>
          ))}
          {lists.length === 0 && childFolders.length === 0 ? (
            <button
              type="button"
              onClick={() => onAddList(folder)}
              className="flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-left text-[0.75rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-seal"
            >
              <IconPlus size={12} />
              新建清单
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function ListRow({
  list,
  active,
  onSelect,
  onEdit,
  menu,
  setMenu,
  folders,
}: {
  list: List
  active: boolean
  onSelect: () => void
  onEdit: () => void
  menu: string | null
  setMenu: (k: string | null) => void
  folders?: Folder[]
}) {
  const { updateList } = useStore()
  const { item: dragItem } = useContext(DragCtx)
  const key = `list-${list.id}`
  const draggingSelf = dragItem?.kind === 'list' && dragItem.id === list.id
  return (
    <div className="group/list relative">
      <div
        className={cx(
          'flex items-center gap-1 rounded-lg pr-1.5 transition-colors hover:bg-surface-2',
          active && 'bg-seal/10',
          draggingSelf && 'opacity-50',
        )}
      >
        <button
          type="button"
          onClick={onSelect}
          className={cx(
            'flex min-w-0 flex-1 items-center gap-2.5 py-[7px] pl-2.5 text-left text-[0.8125rem]',
            active ? 'font-medium text-seal' : 'text-ink-2 hover:text-ink',
          )}
        >
          <ColorDot color={list.color} />
          <span className="truncate">{list.name}</span>
          {/* 清单角标数的是任务，与分组的「N 清单」区分量纲。 */}
          {list.taskCount ? <span className="text-[0.65625rem] text-ink-3 tabular-nums">{list.taskCount} 任务</span> : null}
        </button>
        <span className="opacity-0 transition-opacity group-hover/list:opacity-100">
          <IconGrip size={12} className="text-ink-3/50" />
        </span>
        <IconButton
          icon={IconMore}
          label={`「${list.name}」的更多操作`}
          size={13}
          aria-expanded={menu === key}
          className="opacity-0 transition-opacity group-hover/list:opacity-100"
          onClick={(e) => {
            e.stopPropagation()
            setMenu(menu === key ? null : key)
          }}
        />
      </div>
      <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={186}>
        <MenuItem
          icon={IconPencil}
          onClick={() => {
            onEdit()
            setMenu(null)
          }}
        >
          编辑清单…
        </MenuItem>
        <MenuItem
          icon={IconStar}
          onClick={() => {
            void updateList(list.id, { starred: !list.starred })
            setMenu(null)
          }}
        >
          {list.starred ? '取消收藏' : '收藏'}
        </MenuItem>
        <MenuItem
          icon={list.archived ? IconList : IconArchive}
          onClick={() => {
            void updateList(list.id, { archived: !list.archived })
            setMenu(null)
          }}
        >
          {list.archived ? '取消归档' : '归档'}
        </MenuItem>
        {folders && folders.length > 0 ? (
          <>
            <div className="my-1 border-t border-line" />
            <div className="px-2.5 py-1 text-[0.65625rem] tracking-wide text-ink-3">移动到分组</div>
            <MenuItem
              onClick={() => {
                void updateList(list.id, { moveToRoot: true })
                setMenu(null)
              }}
            >
              移出分组
            </MenuItem>
            {flattenFolders(folders).map(({ folder: f, depth }) => (
              <MenuItem
                key={f.id}
                icon={() => <ColorDot color={f.color} />}
                onClick={() => {
                  void updateList(list.id, { folderId: f.id })
                  setMenu(null)
                }}
              >
                {depth > 0 ? `${'　'.repeat(depth)}${f.name}` : f.name}
              </MenuItem>
            ))}
          </>
        ) : null}
      </Popover>
    </div>
  )
}

/* ---------------- 外观 ---------------- */

const THEME_CHOICES: { value: ThemeMode; label: string }[] = [
  { value: 'light', label: '素笺 · 浅色' },
  { value: 'dark', label: '夜砚 · 深色' },
  { value: 'auto', label: '随系统 · 自动' },
]

/** 通知状态的口语说法，和后台的真实 Permission 值一一对应。 */
const NOTIFY_LABEL: Record<NotifyState, string> = {
  granted: '已允许',
  denied: '已被拒绝',
  default: '未授权',
  unsupported: '不支持',
  insecure: '非安全上下文',
  framed: '被框架限制',
}

function AppearanceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { settings, saveSettings, toast, confirm, themeMode, resolvedTheme } = useStore()
  const { diag, request } = useNotifyDiagnosis()
  const scale = fontScaleOf(settings.fontScale)
  const fileRef = useRef<HTMLInputElement>(null)
  const [integrationsOpen, setIntegrationsOpen] = useState(false)
  // 文件选择框无法回传「用户点了哪个按钮」，用 ref 记住本次导入的意图。
  const pendingMode = useRef<ImportMode>('merge')

  /** 申请通知权限。授权成功后顺手发一条测试通知——它能一次性暴露「浏览器给了权限但系统级别拦着」的情况。 */
  const askNotification = async () => {
    const r = await request()
    if (r === 'granted') {
      const shown = pushNotification(
        '慎始 · 通知已开启',
        '这是一条测试通知。若它没有出现在 macOS 通知中心，请检查「系统设置 → 通知」里该浏览器是否被允许。',
      )
      toast(shown ? '桌面通知已开启，并发送了一条测试通知' : '已授权，但通知未能发出，请检查系统通知设置', shown ? 'ok' : 'info')
    } else {
      toast(`未能开启桌面通知：${diagnoseNotifications().label}`, 'info')
    }
  }

  const pickFile = (mode: ImportMode) => {
    pendingMode.current = mode
    if (!fileRef.current) return
    fileRef.current.value = '' // 允许连续选择同一个文件
    fileRef.current.click()
  }

  const handleFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const mode = pendingMode.current
    const isZip = /\.zip$/i.test(file.name)

    // 压缩包交给服务端解：附件是字节流，前端拆开再拼回 multipart 没有意义。
    let bundle: unknown = null
    if (!isZip) {
      try {
        bundle = JSON.parse(await file.text())
      } catch {
        toast('这个文件不是有效的 JSON 备份', 'error')
        return
      }
    }

    const ok = await confirm({
      title: mode === 'replace' ? '覆盖导入' : '追加导入',
      message:
        mode === 'replace'
          ? `将清空现有全部数据，并用「${file.name}」重建。此操作不可撤销。`
          : `将把「${file.name}」作为副本追加进来，现有数据不会被动到。`,
      confirmText: mode === 'replace' ? '清空并导入' : '追加导入',
      danger: mode === 'replace',
    })
    if (!ok) return
    try {
      const res = isZip
        ? await api.importBackupFile(file, mode)
        : await api.importBackup(bundle as object, mode)
      const missing = res.attachmentsMissed ?? 0
      toast(
        `导入完成：${res.tasks} 件任务、${res.lists} 个清单` +
          (res.attachments ? `、${res.attachments} 个附件` : '') +
          (missing ? `（${missing} 个附件没带来文件，未恢复）` : ''),
        missing ? 'info' : 'ok',
      )
      onClose()
      // 备份可能替换了清单与设置，整页重载最稳妥。
      window.setTimeout(() => window.location.reload(), 500)
    } catch (err) {
      toast(err instanceof Error ? err.message : '导入失败', 'error')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="外观与设置" width={900}>
      <div className="space-y-4">
        {/* 两栏排布：常用项并排，弹窗本身也能占满可用高度，多数情况下不必滚动。 */}
        <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
          <div className="space-y-4">
            <Field
              label="明暗"
              hint={themeMode === 'auto' ? `跟随系统，当前为${resolvedTheme === 'dark' ? '深色' : '浅色'}` : undefined}
            >
              <div className="grid grid-cols-3 gap-2">
                {THEME_CHOICES.map((t) => (
                  <button
                    key={t.value}
                    type="button"
                    data-theme-choice={t.value}
                    onClick={() => void saveSettings({ theme: t.value })}
                    className={cx(
                      'rounded-lg border px-2 py-2 text-[0.8125rem] transition-colors',
                      themeMode === t.value
                        ? 'border-seal bg-seal/10 text-seal'
                        : 'border-line text-ink-2 hover:bg-surface-2',
                    )}
                  >
                    {t.label}
                  </button>
                ))}
              </div>
            </Field>

            <Field label="界面字号" hint="正文与间距一起缩放，立即生效。">
              <div className="grid grid-cols-4 gap-2">
                {FONT_SCALES.map((s) => (
                  <button
                    key={s.value}
                    type="button"
                    data-font-scale={s.value}
                    onClick={() => void saveSettings({ fontScale: s.value })}
                    className={cx(
                      'rounded-lg border px-1 py-1.5 transition-colors',
                      scale === fontScaleOf(s.value)
                        ? 'border-seal bg-seal/10 text-seal'
                        : 'border-line text-ink-2 hover:bg-surface-2',
                    )}
                  >
                    <span className="block text-[0.78125rem]">{s.label}</span>
                    <span className="block text-[0.65625rem] text-ink-3">{s.percent}</span>
                  </button>
                ))}
              </div>
            </Field>
          </div>

          <div className="space-y-4">
            <Field label="印章色" hint="影响按钮、选中态与强调色，不改动结构。">
              <div className="flex flex-wrap gap-2">
                {ACCENTS.map((a) => (
                  <button
                    key={a.value}
                    type="button"
                    onClick={() => void saveSettings({ accent: a.value })}
                    className={cx(
                      'flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[0.78125rem] transition-colors',
                      (settings.accent ?? 'seal') === a.value
                        ? 'border-seal bg-seal/10 text-ink'
                        : 'border-line text-ink-2 hover:bg-surface-2',
                    )}
                  >
                    <span className="h-3.5 w-3.5 rounded-full" style={{ background: a.color }} />
                    {a.label}
                  </button>
                ))}
              </div>
            </Field>

            <Field label="任务列表" hint="归档的任务收进侧栏「已归档」，随时可恢复。">
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-[0.78125rem] text-ink-2">
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5 accent-[var(--seal)]"
                    checked={settings.showCompleted !== '0'}
                    onChange={(e) => void saveSettings({ showCompleted: e.target.checked ? '1' : '0' })}
                  />
                  显示已完成任务
                </label>
                <p className="text-[0.6875rem] leading-relaxed text-ink-3">
                  关闭后日常视图只看未完成的事；「已完成 / 最近完成」清单不受影响。
                </p>
              </div>
            </Field>

            <Field label="提醒与提示音">
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-[0.78125rem] text-ink-2">
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5 accent-[var(--seal)]"
                    checked={settings.soundOn !== '0'}
                    onChange={(e) => void saveSettings({ soundOn: e.target.checked ? '1' : '0' })}
                  />
                  提醒时播放提示音
                </label>
                <div className="flex flex-wrap items-center gap-2">
                  <Button variant="outline" size="sm" disabled={diag.state === 'granted'} onClick={() => void askNotification()}>
                    {diag.state === 'granted' ? '桌面通知已开启' : '申请桌面通知权限'}
                  </Button>
                  <span className={cx('text-[0.71875rem]', diag.state === 'granted' ? 'text-jade' : 'text-ink-3')}>
                    当前：{NOTIFY_LABEL[diag.state]}
                  </span>
                  {diag.state === 'framed' ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => window.open(window.location.href, '_blank', 'noopener')}
                    >
                      在新窗口打开
                    </Button>
                  ) : null}
                </div>
                {/* 权限一旦被浏览器记成拒绝，网页就再也弹不出授权框。把原因和改法写在面板上，别让用户反复点。 */}
                <p className="text-[0.6875rem] leading-relaxed text-ink-3">
                  {diag.state === 'granted' ? '提醒会同时出现在系统通知中心。' : diag.label}
                  {diag.advice ? ` ${diag.advice}` : ''}
                </p>
              </div>
            </Field>
          </div>
        </div>

        {/* 底部四块两栏排布：数据｜集成 / 快捷键｜重置提醒，压缩整体高度。 */}
        <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
          <Field label="数据" hint="备份是自洽的：清单、标签、子任务、复盘、专注记录都在其中。">
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                data-export-zip
                onClick={() => downloadExport(EXPORT_URLS.zip)}
              >
                导出完整备份 ZIP
              </Button>
              <Button variant="outline" size="sm" onClick={() => downloadExport(EXPORT_URLS.json)}>
                仅 JSON
              </Button>
              <Button variant="outline" size="sm" onClick={() => downloadExport(EXPORT_URLS.csv)}>
                导出任务表 CSV
              </Button>
              <Button variant="outline" size="sm" onClick={() => pickFile('merge')}>
                导入（追加）
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-p-high"
                onClick={() => pickFile('replace')}
              >
                导入（覆盖）
              </Button>
            </div>
            <p className="text-[0.6875rem] leading-relaxed text-ink-3">
              ZIP 是完整备份：JSON 加上任务附件，一份就能还原全部。CSV 只含任务表，便于在表格软件里查阅。
              「追加」保留现有数据，「覆盖」会先清空再重建。
            </p>
          </div>
          <input
            ref={fileRef}
            type="file"
            data-import-input
            accept=".zip,.json,application/zip,application/json"
            className="hidden"
            onChange={(e) => void handleFile(e)}
          />
        </Field>

        <Field label="快捷键">
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-[0.75rem] text-ink-2">
            {[
              ['N', '新建任务'],
              ['/', '搜索'],
              ['T', '今天'],
              ['I', '收集箱'],
              ['C', '日历'],
              ['Q', '四象限'],
              ['B', '看板'],
              ['H', '习惯打卡'],
              ['S', '统计与复盘'],
              ['Esc', '关闭面板 / 取消'],
            ].map(([k, v]) => (
              <div key={k} className="flex items-center gap-2">
                <kbd className="rounded border border-line bg-surface-2 px-1.5 py-0.5 font-mono text-[0.65625rem] text-ink-3">
                  {k}
                </kbd>
                <span>{v}</span>
              </div>
            ))}
          </div>
        </Field>

        <div className="flex items-center justify-between gap-4">
          <span className="text-[0.75rem] leading-relaxed text-ink-3">
            模板任务、Webhook、自动备份与 CalDAV 订阅。
          </span>
          <Button
            variant="outline"
            size="sm"
            className="shrink-0 whitespace-nowrap"
            data-open-integrations
            onClick={() => setIntegrationsOpen(true)}
          >
            集成与自动化
          </Button>
        </div>

        <div className="flex items-center justify-between gap-4">
          <span className="text-[0.75rem] leading-relaxed text-ink-3">
            清空提醒台账后，已提醒过的事项会重新参与提醒。
          </span>
          <Button
            variant="outline"
            size="sm"
            className="shrink-0 whitespace-nowrap"
            onClick={async () => {
              const ok = await confirm({
                title: '重置提醒台账',
                message: '已提醒过的事项会重新参与提醒，可能立刻收到一批通知。',
                confirmText: '重置',
                danger: true,
              })
              if (!ok) return
              try {
                await api.resetReminders()
                toast('提醒台账已重置')
              } catch (e) {
                // 原先没有 catch：失败时按钮静默无反应，用户会反复点
                toast(e instanceof Error ? e.message : '重置失败，请稍后重试', 'error')
              }
            }}
          >
            重置提醒
          </Button>
        </div>
        </div>

        <IntegrationsDialog open={integrationsOpen} onClose={() => setIntegrationsOpen(false)} />
      </div>
    </Modal>
  )
}

/* ---------------- 操作历史 ---------------- */

function HistoryDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { activities, clearActivities, toast, confirm } = useStore()
  const clear = async () => {
    const ok = await confirm({
      title: '清空操作历史',
      message: '历史记录仅作回顾，清空后不可恢复。',
      confirmText: '清空',
      danger: true,
    })
    if (!ok) return
    try {
      await clearActivities()
      toast('操作历史已清空')
    } catch {
      toast('清空失败，请重试', 'error')
    }
  }
  return (
    <Modal open={open} onClose={onClose} title="操作历史" width={440}>
      <div className="flex min-h-[200px] flex-col">
        {activities.length === 0 ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 py-10 text-center text-ink-3">
            <IconHistory size={28} className="opacity-50" />
            <p className="text-[0.78125rem]">还没有记录。新建、完成、删除等动作会留在这里。</p>
          </div>
        ) : (
          <ul className="max-h-[60vh] space-y-0.5 overflow-y-auto pr-1">
            {activities.map((a) => (
              <li key={a.id} className="flex items-start gap-2.5 rounded-lg px-2 py-1.5 text-[0.78125rem] hover:bg-surface-2">
                <span className="mt-[2px] inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-surface-2 px-1.5 text-[0.65625rem] text-ink-2">
                  {ACTIVITY_LABEL[a.kind] ?? a.kind}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-ink">{a.title}</span>
                  {a.detail ? <span className="block truncate text-[0.6875rem] text-ink-3">{a.detail}</span> : null}
                </span>
                <span className="shrink-0 whitespace-nowrap text-[0.65625rem] text-ink-3 tabular-nums">
                  {relativeTime(a.createdAt)}
                </span>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-2 flex items-center justify-between border-t border-line pt-3">
          <span className="text-[0.6875rem] text-ink-3">仅保留最近若干条，作为回顾之用</span>
          <Button variant="outline" size="sm" className="text-p-high" icon={IconTrash} onClick={clear} disabled={activities.length === 0}>
            清空历史
          </Button>
        </div>
      </div>
    </Modal>
  )
}
