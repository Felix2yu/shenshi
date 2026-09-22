import { useMemo, useRef, useState, type ChangeEvent, type DragEvent, type ReactNode } from 'react'

import { EXPORT_URLS, api, type ImportMode } from '../api/client'
import { humanDay, todayStr } from '../lib/date'
import { requestPermission } from '../lib/notify'
import { ACCENTS, PALETTE } from '../lib/palette'
import { useStore } from '../store/AppStore'
import type { Folder, List, SmartKey, Tag } from '../types'
import { SORT_MIME } from './TaskViews'
import {
  IconBell,
  IconBook,
  IconCalendar,
  IconChart,
  IconChevronDown,
  IconChevronRight,
  IconCircle,
  IconColumns,
  IconGrip,
  IconGrid,
  IconInbox,
  IconList,
  IconMoon,
  IconMore,
  IconPencil,
  IconPlus,
  IconSearch,
  IconSeedling,
  IconSettings,
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
 * 侧栏同层拖拽排序。只处理「平级之间调顺序」，跨层移动仍走编辑弹窗，
 * 这样既满足排序需求，又不会让一次误拖把清单换到别的分组里。
 * 返回的 props 直接展开到每一行的容器上即可。
 */
function useRowSort(ids: number[], onReorder: (next: number[]) => void) {
  const [dragId, setDragId] = useState<number | null>(null)
  const [overId, setOverId] = useState<number | null>(null)

  const propsFor = (id: number) => ({
    draggable: true,
    title: '按住拖动可调整顺序',
    onDragStart: (e: DragEvent<HTMLDivElement>) => {
      // 分组与清单嵌套，必须阻止冒泡，否则拖清单会连带触发外层分组。
      e.stopPropagation()
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData(SORT_MIME, String(id))
      setDragId(id)
    },
    onDragEnd: () => {
      setDragId(null)
      setOverId(null)
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

/* ---------------- 智能清单与视图定义 ---------------- */

const SMARTS: { key: SmartKey; label: string; icon: IconCmp; countKey: string; ember?: boolean }[] = [
  { key: 'inbox', label: '收集箱', icon: IconInbox, countKey: 'inbox' },
  { key: 'today', label: '今天', icon: IconSun, countKey: 'today' },
  { key: 'next7', label: '最近 7 天', icon: IconCalendar, countKey: 'next7' },
  { key: 'overdue', label: '逾期', icon: IconBell, countKey: 'overdue', ember: true },
  { key: 'nodate', label: '无日期', icon: IconCircle, countKey: 'nodate' },
  { key: 'all', label: '全部任务', icon: IconList, countKey: 'all' },
  { key: 'done', label: '已完成', icon: IconGrid, countKey: 'done' },
]

const VIEWS: { key: 'board' | 'calendar' | 'quadrant' | 'habits' | 'stats'; label: string; icon: IconCmp }[] = [
  { key: 'board', label: '看板', icon: IconColumns },
  { key: 'calendar', label: '日历', icon: IconCalendar },
  { key: 'quadrant', label: '四象限', icon: IconGrid },
  { key: 'habits', label: '习惯打卡', icon: IconSeedling },
  { key: 'stats', label: '统计与复盘', icon: IconChart },
]

/* ---------------- 新建 / 编辑弹窗 ---------------- */

type EntityKind = 'folder' | 'list' | 'tag'

interface EntityDraft {
  kind: EntityKind
  id?: number
  name: string
  color: string
  folderId?: number | null
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

  const [name, setName] = useState(draft?.name ?? '')
  const [color, setColor] = useState(draft?.color ?? PALETTE[0])
  const [folderId, setFolderId] = useState<number | null>(draft?.folderId ?? null)
  const [busy, setBusy] = useState(false)

  if (!draft) return null
  const isNew = draft.id === undefined
  const kindLabel = draft.kind === 'folder' ? '分组' : draft.kind === 'list' ? '清单' : '标签'

  const submit = async () => {
    const value = name.trim()
    if (!value || busy) return
    setBusy(true)
    try {
      if (draft.kind === 'folder') {
        if (isNew) await createFolder(value, color)
        else await updateFolder(draft.id!, { name: value, color })
      } else if (draft.kind === 'list') {
        if (isNew) await createList(value, folderId ?? undefined, color)
        else await updateList(draft.id!, { name: value, color, folderId: folderId ?? undefined, moveToRoot: folderId === null })
      } else if (isNew) {
        await createTag(value, color)
      } else {
        await updateTag(draft.id!, { name: value, color })
      }
      onClose()
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (isNew) return
    const note =
      draft.kind === 'folder'
        ? '分组内的清单会回到顶层，其中的任务不受影响。'
        : draft.kind === 'list'
          ? '清单中的所有任务会被一并删除，此操作不可撤销。'
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
            className={inputClass}
            value={name}
            placeholder={draft.kind === 'folder' ? '如：工作' : draft.kind === 'list' ? '如：项目推进' : '如：深度工作'}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submit()
            }}
          />
        </Field>

        {draft.kind === 'list' ? (
          <Field label="所属分组">
            <select
              className={inputClass}
              value={folderId ?? ''}
              onChange={(e) => setFolderId(e.target.value ? Number(e.target.value) : null)}
            >
              <option value="">不归入分组</option>
              {folders.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
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
                aria-label={c}
              />
            ))}
          </div>
        </Field>
      </div>
    </Modal>
  )
}

/* ---------------- 侧边栏 ---------------- */

export function Sidebar() {
  const {
    selection,
    select,
    selectSmart,
    setView,
    view,
    counts,
    lists,
    folders,
    tags,
    settings,
    saveSettings,
    updateFolder,
    reorderFolders,
    reorderLists,
    updateTag,
    createTag,
    deleteTag,
    confirm,
    boot,
    reminders,
  } = useStore()

  const [draft, setDraft] = useState<EntityDraft | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const [addingTag, setAddingTag] = useState(false)
  const [newTagName, setNewTagName] = useState('')
  const [appearanceOpen, setAppearanceOpen] = useState(false)

  const inboxId = boot?.inboxListId ?? 0
  const rootLists = useMemo(() => lists.filter((l) => l.id !== inboxId && l.folderId === null), [lists, inboxId])

  // 分组与顶层清单各有一份排序控制器；分组内的清单在 FolderNode 内部单独维护。
  const folderSort = useRowSort(
    folders.map((f) => f.id),
    reorderFolders,
  )
  const rootListSort = useRowSort(
    rootLists.map((l) => l.id),
    reorderLists,
  )

  const smartActive = (key: SmartKey) => selection.kind === 'smart' && selection.key === key && view === 'list'
  const isView = (key: string) => view === key

  const viewTitle = useMemo(() => {
    if (view === 'board') return '看板'
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

  const theme = settings.theme ?? 'light'

  return (
    <>
      <aside className="relative z-20 flex h-full w-[266px] shrink-0 flex-col border-r border-line bg-surface/72">
        {/* 品牌 */}
        <div className="flex items-center gap-2.5 px-4 pb-3 pt-4">
          <SealLogo size={32} />
          <div className="min-w-0 flex-1">
            <div className="brand-serif text-[17px] font-semibold leading-none text-ink">慎始</div>
            <div className="mt-1 truncate text-[10.5px] tracking-wide text-ink-3">慎始而敬终 · 行稳致远</div>
          </div>
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
              <span className="brand-serif block text-[13px] font-medium text-ink">晨省 · 规划今日</span>
              <span className="block truncate text-[10.5px] text-ink-3">凡事豫则立，不豫则废</span>
            </span>
            <IconChevronRight size={14} className="shrink-0 text-seal/60 transition-transform group-hover:translate-x-0.5" />
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 pb-3">
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
                    'group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left text-[13px] transition-colors',
                    active ? 'bg-seal/10 font-medium text-seal' : 'text-ink-2 hover:bg-surface-2 hover:text-ink',
                  )}
                >
                  <s.icon size={15} className={cx('shrink-0', active ? 'text-seal' : 'text-ink-3')} />
                  <span className="flex-1 truncate">{s.label}</span>
                  {s.key === 'today' ? <span className="text-[10.5px] text-ink-3">{humanDay(todayStr())}</span> : null}
                  {n > 0 ? (
                    <span
                      className={cx(
                        'min-w-4 rounded-full px-1.5 text-center text-[10.5px] tabular-nums',
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

          {/* 视图 */}
          <div className="mt-1 border-t border-line pt-1">
            {VIEWS.map((v) => {
              const active = isView(v.key)
              return (
                <button
                  key={v.key}
                  type="button"
                  onClick={() => {
                    if (v.key === 'quadrant') {
                      // 四象限天然是全局视图，先切到「全部任务」再换视图。
                      select({ kind: 'smart', key: 'all' })
                      setView('quadrant')
                    } else {
                      setView(v.key)
                    }
                  }}
                  className={cx(
                    'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left text-[13px] transition-colors',
                    active ? 'bg-seal/10 font-medium text-seal' : 'text-ink-2 hover:bg-surface-2 hover:text-ink',
                  )}
                >
                  <v.icon size={15} className={cx('shrink-0', active ? 'text-seal' : 'text-ink-3')} />
                  <span className="flex-1 truncate">{v.label}</span>
                </button>
              )
            })}
          </div>

          {/* 分组 */}
          <div className="mt-1 border-t border-line pt-1">
            <SideHeader
              label="分组"
              onAdd={() => setDraft({ kind: 'folder', name: '', color: PALETTE[2] })}
              addLabel="新建分组"
            />
            {folders.map((f) => (
              <div
                key={f.id}
                {...folderSort.propsFor(f.id)}
                className={cx('rounded-lg', dragClass(folderSort.overId === f.id))}
              >
                <FolderNode
                  folder={f}
                  lists={lists.filter((l) => l.folderId === f.id)}
                  selection={selection}
                  view={view}
                  menu={menu}
                  setMenu={setMenu}
                  onSelectFolder={() => select({ kind: 'folder', id: f.id })}
                  onSelectList={(id) => select({ kind: 'list', id })}
                  onToggleCollapse={() => void updateFolder(f.id, { collapsed: !f.collapsed })}
                  onEdit={() => setDraft({ kind: 'folder', id: f.id, name: f.name, color: f.color })}
                  onAddList={() => setDraft({ kind: 'list', name: '', color: f.color, folderId: f.id })}
                  onReorderLists={reorderLists}
                />
              </div>
            ))}

            <SideHeader label="清单" onAdd={() => setDraft({ kind: 'list', name: '', color: PALETTE[0], folderId: null })} addLabel="新建清单" />
            {rootLists.map((l) => (
              <div
                key={l.id}
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
                  folders={folders}
                />
              </div>
            ))}
            {rootLists.length === 0 && folders.length === 0 ? (
              <p className="px-2.5 py-1 text-[11.5px] text-ink-3">还没有清单，点标题右侧的 + 开始。</p>
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
                  await createTag(name)
                  setNewTagName('')
                  setAddingTag(false)
                }}
              >
                <input
                  autoFocus
                  className="min-w-0 flex-1 rounded-md border border-line bg-surface px-2 py-1 text-[12.5px] outline-none focus:border-seal/60"
                  placeholder="标签名"
                  value={newTagName}
                  onChange={(e) => setNewTagName(e.target.value)}
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
                        'flex min-w-0 flex-1 items-center gap-2.5 py-[7px] pl-2.5 text-left text-[13px]',
                        active ? 'font-medium text-seal' : 'text-ink-2 hover:text-ink',
                      )}
                    >
                      <IconTag size={14} className="shrink-0" style={{ color: t.color }} />
                      <span className="truncate">{t.name}</span>
                      {t.taskCount ? <span className="text-[10.5px] text-ink-3 tabular-nums">{t.taskCount}</span> : null}
                    </button>
                    <span
                      role="button"
                      tabIndex={-1}
                      className="opacity-0 transition-opacity group-hover/tag:opacity-100"
                      onClick={(e) => {
                        e.stopPropagation()
                        setMenu(menu === key ? null : key)
                      }}
                    >
                      <IconMore size={13} className="text-ink-3" />
                    </span>
                  </div>
                  <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={150}>
                    <div className="flex flex-wrap gap-1 px-1.5 py-1.5">
                      {PALETTE.map((c) => (
                        <button
                          key={c}
                          type="button"
                          aria-label={c}
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
                        删除标签
                      </MenuItem>
                    </div>
                  </Popover>
                </div>
              )
            })}
            {tags.length === 0 && !addingTag ? (
              <p className="px-2.5 py-1 text-[11.5px] leading-relaxed text-ink-3">
                在任务标题里输入 <span className="text-seal">#标签</span> 即可自动创建。
              </p>
            ) : null}
          </div>
        </nav>

        {/* 底部 */}
        <div className="border-t border-line px-3 py-2">
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => window.dispatchEvent(new CustomEvent('shenshi:review'))}
              className="flex flex-1 items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[12.5px] text-ink-2 transition-colors hover:bg-surface-2 hover:text-ink"
            >
              <IconBook size={14} className="text-ink-3" />
              日省 · 今日复盘
            </button>
            <IconButton
              icon={theme === 'dark' ? IconSun : IconMoon}
              label={theme === 'dark' ? '切换到浅色' : '切换到深色'}
              onClick={() => void saveSettings({ theme: theme === 'dark' ? 'light' : 'dark' })}
            />
            <IconButton
              icon={IconTimer}
              label="专注计时"
              onClick={() => window.dispatchEvent(new CustomEvent('shenshi:focus'))}
            />
          </div>
          <div className="mt-1 flex items-center gap-1.5 px-2.5 text-[10.5px] text-ink-3">
            <IconBell size={12} />
            <span className="truncate">{reminders.length > 0 ? `${reminders.length} 条提醒待处理` : '提醒已就绪'}</span>
            <span className="ml-auto shrink-0 tabular-nums">{viewTitle}</span>
          </div>
        </div>
      </aside>

      <EntityDialog key={draft ? `${draft.kind}-${draft.id ?? 'new'}` : 'none'} draft={draft} onClose={() => setDraft(null)} />
      <AppearanceDialog open={appearanceOpen} onClose={() => setAppearanceOpen(false)} />
    </>
  )
}

/* ---------------- 小组件 ---------------- */

function SideHeader({ label, onAdd, addLabel }: { label: string; onAdd: () => void; addLabel: string }) {
  return (
    <div className="flex items-center justify-between px-2 pb-1 pt-3">
      <span className="text-[10.5px] font-semibold uppercase tracking-[0.14em] text-ink-3">{label}</span>
      <IconButton icon={IconPlus} label={addLabel} size={13} onClick={onAdd} />
    </div>
  )
}

function FolderNode({
  folder,
  lists,
  selection,
  view,
  menu,
  setMenu,
  onSelectFolder,
  onSelectList,
  onToggleCollapse,
  onEdit,
  onAddList,
  onReorderLists,
}: {
  folder: Folder
  lists: List[]
  selection: ReturnType<typeof useStore>['selection']
  view: string
  menu: string | null
  setMenu: (k: string | null) => void
  onSelectFolder: () => void
  onSelectList: (id: number) => void
  onToggleCollapse: () => void
  onEdit: () => void
  onAddList: () => void
  onReorderLists: (ids: number[]) => void
}) {
  const key = `folder-${folder.id}`
  const active = selection.kind === 'folder' && selection.id === folder.id && view === 'list'
  const listSort = useRowSort(
    lists.map((l) => l.id),
    onReorderLists,
  )
  return (
    <div>
      <div
        className={cx(
          'group/folder flex items-center gap-0.5 rounded-lg pl-0.5 pr-1.5 transition-colors hover:bg-surface-2',
          active && 'bg-seal/10',
        )}
      >
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={folder.collapsed ? '展开分组' : '折叠分组'}
          className="grid h-6 w-5 shrink-0 place-items-center rounded text-ink-3 hover:text-ink"
        >
          {folder.collapsed ? <IconChevronRight size={12} /> : <IconChevronDown size={12} />}
        </button>
        <button
          type="button"
          onClick={onSelectFolder}
          className={cx(
            'flex min-w-0 flex-1 items-center gap-2 py-[7px] text-left text-[13px]',
            active ? 'font-medium text-seal' : 'text-ink',
          )}
        >
          <ColorDot color={folder.color} />
          <span className="truncate">{folder.name}</span>
          <span className="text-[10.5px] text-ink-3 tabular-nums">{lists.length || ''}</span>
        </button>
        <span className="opacity-0 transition-opacity group-hover/folder:opacity-100">
          <IconGrip size={12} className="text-ink-3/50" />
        </span>
        <span
          role="button"
          tabIndex={-1}
          className="opacity-0 transition-opacity group-hover/folder:opacity-100"
          onClick={(e) => {
            e.stopPropagation()
            setMenu(menu === key ? null : key)
          }}
        >
          <IconMore size={13} className="text-ink-3" />
        </span>
        <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={172}>
          <MenuItem
            icon={IconPlus}
            onClick={() => {
              onAddList()
              setMenu(null)
            }}
          >
            在此新建清单
          </MenuItem>
          <MenuItem
            icon={IconPencil}
            onClick={() => {
              onEdit()
              setMenu(null)
            }}
          >
            重命名与配色
          </MenuItem>
          <MenuItem
            icon={folder.collapsed ? IconChevronDown : IconChevronRight}
            onClick={() => {
              onToggleCollapse()
              setMenu(null)
            }}
          >
            {folder.collapsed ? '展开分组' : '折叠分组'}
          </MenuItem>
          <MenuItem
            icon={IconTrash}
            danger
            onClick={() => {
              onEdit()
              setMenu(null)
            }}
          >
            管理分组…
          </MenuItem>
        </Popover>
      </div>

      {!folder.collapsed ? (
        <div className="ml-[15px] border-l border-line pl-1.5">
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
                onEdit={onEdit}
                menu={menu}
                setMenu={setMenu}
              />
            </div>
          ))}
          {lists.length === 0 ? (
            <button
              type="button"
              onClick={onAddList}
              className="flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-left text-[12px] text-ink-3 transition-colors hover:bg-surface-2 hover:text-seal"
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
  const key = `list-${list.id}`
  return (
    <div className="group/list relative">
      <div
        className={cx('flex items-center gap-1 rounded-lg pr-1.5 transition-colors hover:bg-surface-2', active && 'bg-seal/10')}
      >
        <button
          type="button"
          onClick={onSelect}
          className={cx(
            'flex min-w-0 flex-1 items-center gap-2.5 py-[7px] pl-2.5 text-left text-[13px]',
            active ? 'font-medium text-seal' : 'text-ink-2 hover:text-ink',
          )}
        >
          <ColorDot color={list.color} />
          <span className="truncate">{list.name}</span>
          {list.taskCount ? <span className="text-[10.5px] text-ink-3 tabular-nums">{list.taskCount}</span> : null}
        </button>
        <span className="opacity-0 transition-opacity group-hover/list:opacity-100">
          <IconGrip size={12} className="text-ink-3/50" />
        </span>
        <span
          role="button"
          tabIndex={-1}
          className="opacity-0 transition-opacity group-hover/list:opacity-100"
          onClick={(e) => {
            e.stopPropagation()
            setMenu(menu === key ? null : key)
          }}
        >
          <IconMore size={13} className="text-ink-3" />
        </span>
      </div>
      <Popover open={menu === key} onClose={() => setMenu(null)} align="right" width={186}>
        <MenuItem
          icon={IconPencil}
          onClick={() => {
            onEdit()
            setMenu(null)
          }}
        >
          重命名与配色
        </MenuItem>
        {folders && folders.length > 0 ? (
          <>
            <div className="my-1 border-t border-line" />
            <div className="px-2.5 py-1 text-[10.5px] tracking-wide text-ink-3">移动到分组</div>
            <MenuItem
              onClick={() => {
                void updateList(list.id, { moveToRoot: true })
                setMenu(null)
              }}
            >
              不归入分组
            </MenuItem>
            {folders.map((f) => (
              <MenuItem
                key={f.id}
                icon={() => <ColorDot color={f.color} />}
                onClick={() => {
                  void updateList(list.id, { folderId: f.id })
                  setMenu(null)
                }}
              >
                {f.name}
              </MenuItem>
            ))}
          </>
        ) : null}
      </Popover>
    </div>
  )
}

/* ---------------- 外观 ---------------- */

function AppearanceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { settings, saveSettings, toast, confirm } = useStore()
  const theme = settings.theme ?? 'light'
  const fileRef = useRef<HTMLInputElement>(null)
  const [integrationsOpen, setIntegrationsOpen] = useState(false)
  // 文件选择框无法回传「用户点了哪个按钮」，用 ref 记住本次导入的意图。
  const pendingMode = useRef<ImportMode>('merge')

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
    <Modal open={open} onClose={onClose} title="外观与设置" width={460} size="lg">
      <div className="space-y-5">
        <Field label="明暗">
          <div className="flex gap-2">
            {(['light', 'dark'] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => void saveSettings({ theme: t })}
                className={cx(
                  'flex-1 rounded-lg border px-3 py-2 text-[13px] transition-colors',
                  theme === t ? 'border-seal bg-seal/10 text-seal' : 'border-line text-ink-2 hover:bg-surface-2',
                )}
              >
                {t === 'light' ? '素笺 · 浅色' : '夜砚 · 深色'}
              </button>
            ))}
          </div>
        </Field>

        <Field label="印章色" hint="影响按钮、选中态与强调色，不改动结构。">
          <div className="flex flex-wrap gap-2">
            {ACCENTS.map((a) => (
              <button
                key={a.value}
                type="button"
                onClick={() => void saveSettings({ accent: a.value })}
                className={cx(
                  'flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[12.5px] transition-colors',
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

        <Field label="提醒与提示音">
          <div className="space-y-2">
            <label className="flex items-center gap-2 text-[12.5px] text-ink-2">
              <input
                type="checkbox"
                className="h-3.5 w-3.5 accent-[var(--seal)]"
                checked={settings.soundOn !== '0'}
                onChange={(e) => void saveSettings({ soundOn: e.target.checked ? '1' : '0' })}
              />
              提醒时播放提示音
            </label>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={async () => {
                  const r = await requestPermission()
                  toast(
                    r === 'granted'
                      ? '桌面通知已开启'
                      : r === 'denied'
                        ? '桌面通知被拒绝，可在浏览器站点设置中重新允许'
                        : '当前浏览器不支持桌面通知，将只使用应用内提醒',
                    r === 'granted' ? 'ok' : 'info',
                  )
                }}
              >
                申请桌面通知权限
              </Button>
              <span className="text-[11.5px] text-ink-3">
                {typeof Notification === 'undefined' ? '当前环境不支持' : `当前：${Notification.permission}`}
              </span>
            </div>
          </div>
        </Field>

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
            <p className="text-[11px] leading-relaxed text-ink-3">
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

        <div className="flex items-center justify-between gap-4 border-t border-line pt-4">
          <span className="text-[12px] leading-relaxed text-ink-3">
            模板任务、Webhook、自动备份与 CalDAV 订阅。
          </span>
          <Button variant="outline" size="sm" data-open-integrations onClick={() => setIntegrationsOpen(true)}>
            集成与自动化
          </Button>
        </div>

        <Field label="快捷键">
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-[12px] text-ink-2">
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
                <kbd className="rounded border border-line bg-surface-2 px-1.5 py-0.5 font-mono text-[10.5px] text-ink-3">
                  {k}
                </kbd>
                <span>{v}</span>
              </div>
            ))}
          </div>
        </Field>

        <div className="flex items-center justify-between gap-4 border-t border-line pt-4">
          <span className="text-[12px] leading-relaxed text-ink-3">
            清空提醒台账后，已提醒过的事项会重新参与提醒。
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={async () => {
              await api.resetReminders()
              toast('提醒台账已重置')
            }}
          >
            重置提醒
          </Button>
        </div>

        <IntegrationsDialog open={integrationsOpen} onClose={() => setIntegrationsOpen(false)} />
      </div>
    </Modal>
  )
}
