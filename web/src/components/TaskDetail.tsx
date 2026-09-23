import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { api } from '../api/client'
import { addDays, addMonths, dayDiff, fullDate, relativeTime, todayStr, weekday } from '../lib/date'
import { renderMarkdown } from '../lib/markdown'
import { describeRepeat } from '../lib/nlp'
import { useIMEGuard } from '../lib/ime'
import { useStore } from '../store/AppStore'
import type { Attachment, Priority, Subtask, Task } from '../types'
import {
  IconBell,
  IconCalendar,
  IconCalendarRange,
  IconCheck,
  IconClock,
  IconCopy,
  IconDownload,
  IconAlert,
  IconEye,
  IconFlag,
  IconArchive,
  IconLink,
  IconList,
  IconMore,
  IconNote,
  IconPaperclip,
  IconPin,
  IconPlus,
  IconRepeat,
  IconSparkle,
  IconStar,
  IconSubtask,
  IconTag,
  IconTemplate,
  IconTimer,
  IconTrash,
  IconX,
} from './icons'
import { IconButton, Popover, ProgressRing, RoundCheck, cx, inputClass, useAutoGrow, useDebouncedCallback } from './ui'

const REMINDER_OPTIONS: { value: number; label: string }[] = [
  { value: 0, label: '准点' },
  { value: 5, label: '提前 5 分钟' },
  { value: 15, label: '提前 15 分钟' },
  { value: 30, label: '提前 30 分钟' },
  { value: 60, label: '提前 1 小时' },
  { value: 120, label: '提前 2 小时' },
  { value: 1440, label: '提前 1 天' },
]

const PRIORITY_OPTIONS: { value: Priority; label: string; color: string }[] = [
  { value: 0, label: '无', color: 'var(--ink-3)' },
  { value: 1, label: '低', color: 'var(--p-low)' },
  { value: 2, label: '中', color: 'var(--p-mid)' },
  { value: 3, label: '高', color: 'var(--p-high)' },
]

export function TaskDetail({ taskId, onClose }: { taskId: number; onClose: () => void }) {
  const { compositionProps, isComposing } = useIMEGuard()
  const {
    tasks,
    updateTask,
    deleteTask,
    toggleTask,
    addSubtask,
    updateSubtask,
    deleteSubtask,
    addTaskLink,
    removeTaskLink,
    lists,
    tags,
    repeatMeta,
    confirm,
    createTag,
    startFocus,
    skipTask,
    duplicateTask,
    setSelectedTask,
    toast,
    version,
  } = useStore()

  const task = useMemo(() => tasks.find((t) => t.id === taskId) ?? null, [tasks, taskId])

  // 依赖阻塞：单独拉一次 /blocked，不依赖列表接口是否附带 links。
  const [blockers, setBlockers] = useState<Task[]>([])
  useEffect(() => {
    if (!task || task.status === 'done') {
      setBlockers([])
      return
    }
    let alive = true
    void api
      .taskBlocked(task.id)
      .then((r) => {
        if (alive) setBlockers(r.blockers)
      })
      .catch(() => {
        if (alive) setBlockers([])
      })
    return () => {
      alive = false
    }
  }, [task, version])

  const [title, setTitle] = useState(task?.title ?? '')
  const [notes, setNotes] = useState(task?.notes ?? '')
  // 预计时长与进度：拖动/逐格输入期间不落库，防抖一次写入，避免请求队列把滑块弹回旧值。
  const [estimateDraft, setEstimateDraft] = useState<number | null>(null)
  const [progressDraft, setProgressDraft] = useState<number | null>(null)
  const commitEstimate = useDebouncedCallback((v: number) => {
    if (task) void updateTask(task.id, { estimateMinutes: v })
  }, 450)
  const commitProgress = useDebouncedCallback((v: number) => {
    if (task) void updateTask(task.id, { progress: v })
  }, 450)
  // 切换任务时清掉未提交的草稿，防止上一个任务的值串到下一个。
  useEffect(() => {
    setEstimateDraft(null)
    setProgressDraft(null)
  }, [taskId])
  const [subInput, setSubInput] = useState('')
  const [tagInput, setTagInput] = useState('')
  const [tagPopover, setTagPopover] = useState(false)
  const [repeatPopover, setRepeatPopover] = useState(false)
  const [remindPopover, setRemindPopover] = useState(false)
  const [datePopover, setDatePopover] = useState(false)
  const [listPopover, setListPopover] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [linkPopover, setLinkPopover] = useState(false)
  const [linkQuery, setLinkQuery] = useState('')
  const [linkResults, setLinkResults] = useState<Task[]>([])
  // 备注默认就是编辑态；有 Markdown 语法时进详情直接看到渲染结果更有用。
  const [notesMode, setNotesMode] = useState<'edit' | 'preview'>('edit')
  const notesInputRef = useRef<HTMLTextAreaElement>(null)
  const [attachments, setAttachments] = useState<Attachment[]>(task?.attachments ?? [])
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    setTitle(task?.title ?? '')
    setNotes(task?.notes ?? '')
  }, [task?.id, task?.title, task?.notes])

  // 附件以服务端为准：任务被刷新（对账、改别的字段）时同步回来。
  useEffect(() => {
    setAttachments(task?.attachments ?? [])
  }, [task?.attachments])

  const notesRef = useAutoGrow(notes, 360)

  const commitTitle = useDebouncedCallback((value: string) => {
    if (task && value.trim() && value !== task.title) void updateTask(task.id, { title: value.trim() })
  }, 450)

  const commitNotes = useDebouncedCallback((value: string) => {
    if (task && value !== task.notes) void updateTask(task.id, { notes: value })
  }, 550)

  const setDue = useCallback(
    async (dueDate: string | null, dueTime?: string | null) => {
      if (!task) return
      const patch: Record<string, unknown> = { dueDate }
      if (dueTime !== undefined) patch.dueTime = dueTime
      if (dueDate && dueTime === undefined && !task.dueTime) patch.dueTime = '09:00'
      await updateTask(task.id, patch as never)
    },
    [task, updateTask],
  )

  /** 关联候选搜索：跨清单找任务，排除自己与已关联的。 */
  const searchLinkTargets = useCallback(
    async (q: string, selfId: number) => {
      if (!q.trim()) {
        setLinkResults([])
        return
      }
      try {
        const r = await api.listTasks({ q: q.trim(), limit: 8 })
        const linked = new Set([selfId])
        for (const t of tasks) if (t.id === selfId) t.links?.forEach((l) => linked.add(l.linkedTaskId))
        setLinkResults(r.tasks.filter((t) => !linked.has(t.id)))
      } catch {
        setLinkResults([])
      }
    },
    [tasks],
  )

  /** 在备注光标处插入 GFM 检查清单模板。 */
  const insertChecklist = useCallback(() => {
    const el = notesInputRef.current
    const tpl = '- [ ] 第一步\n- [ ] 第二步\n- [ ] 第三步'
    if (!el) {
      setNotes((prev) => (prev ? `${prev}\n${tpl}` : tpl))
      commitNotes(notes ? `${notes}\n${tpl}` : tpl)
      return
    }
    const start = el.selectionStart ?? notes.length
    const end = el.selectionEnd ?? notes.length
    const head = notes.slice(0, start)
    const tail = notes.slice(end)
    const sep = head && !head.endsWith('\n') ? '\n' : ''
    const next = `${head}${sep}${tpl}${tail && !tail.startsWith('\n') ? '\n' : ''}${tail}`
    setNotes(next)
    commitNotes(next)
    requestAnimationFrame(() => {
      el.focus()
      const pos = (head + sep + tpl).length
      el.setSelectionRange(pos, pos)
    })
  }, [notes, commitNotes])

  if (!task) {
    return (
      <div className="flex h-full w-[352px] shrink-0 items-center justify-center border-l border-line bg-surface text-[0.8125rem] text-ink-3">
        任务已不存在
      </div>
    )
  }

  const done = task.status === 'done'
  // 完成度按全树计（后端装配的 subtaskDone/Open 已含子子任务）。
  const subDone = task.subtaskDone
  const subTotal = task.subtaskDone + task.subtaskOpen
  const tagOptions = tags.filter((t) => !task.tags.some((x) => x.id === t.id))
  const repeatOptions = repeatMeta?.presets ?? []

  const addTagByName = async (name: string) => {
    const clean = name.trim().replace(/^#/, '')
    if (!clean) return
    const created = await createTag(clean)
    if (created) {
      await updateTask(task.id, { tagIds: [...task.tags.map((t) => t.id), created.id] })
    }
    setTagInput('')
  }

  const toggleTag = async (id: number) => {
    const has = task.tags.some((t) => t.id === id)
    const next = has ? task.tags.filter((t) => t.id !== id).map((t) => t.id) : [...task.tags.map((t) => t.id), id]
    await updateTask(task.id, { tagIds: next })
  }

  const toggleReminder = async (value: number) => {
    const has = task.reminders.includes(value)
    const next = has ? task.reminders.filter((r) => r !== value) : [...task.reminders, value].sort((a, b) => a - b)
    await updateTask(task.id, { reminders: next })
  }

  const uploadFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return
    setUploading(true)
    let ok = 0
    try {
      for (const file of Array.from(files)) {
        const a = await api.uploadAttachment(task.id, file)
        setAttachments((prev) => (prev.some((x) => x.id === a.id) ? prev : [...prev, a]))
        ok++
      }
      if (ok > 0) toast(ok > 1 ? `已添加 ${ok} 个附件` : '附件已添加')
    } catch (err) {
      toast(err instanceof Error ? err.message : '上传失败', 'error')
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  const removeAttachment = async (a: Attachment) => {
    const confirmed = await confirm({
      title: '删除附件',
      message: `将删除「${a.name}」，文件会从服务端移除，此操作不可撤销。`,
      confirmText: '删除',
      danger: true,
    })
    if (!confirmed) return
    try {
      await api.deleteAttachment(a.id)
      setAttachments((prev) => prev.filter((x) => x.id !== a.id))
    } catch (err) {
      toast(err instanceof Error ? err.message : '删除失败', 'error')
    }
  }

  /** 存为模板：把当前任务的结构抄一份底稿，日后一键铺开。 */
  const saveAsTemplate = async () => {
    try {
      await api.createTemplate({
        name: task.title,
        title: task.title,
        notes: task.notes,
        listId: task.listId,
        priority: task.priority,
        reminders: task.reminders,
        repeatRule: task.repeatRule,
        important: task.important,
        urgent: task.urgent,
        tagIds: task.tags.map((t) => t.id),
        subtasks: task.subtasks.map((s) => s.title),
      })
      toast('已存为模板，可在「集成与自动化」里调整')
    } catch (err) {
      toast(err instanceof Error ? err.message : '保存模板失败', 'error')
    }
  }

  return (
    <aside className="flex h-full w-[356px] shrink-0 animate-slide-left flex-col border-l border-line bg-surface">
      {/* 头部 */}
      <header className="flex items-center gap-1 border-b border-line px-3 py-2">
        <RoundCheck checked={done} onChange={() => void toggleTask(task.id)} size={18} />
        <span className="ml-1 flex-1 truncate text-[0.75rem] text-ink-3">
          {done ? `完成于 ${relativeTime(task.completedAt)}` : `创建于 ${relativeTime(task.createdAt)}`}
        </span>
        <IconButton
          icon={IconPin}
          label={task.pinned ? '取消置顶' : '置顶'}
          active={task.pinned}
          onClick={() => void updateTask(task.id, { pinned: !task.pinned })}
        />
        <IconButton
          icon={IconStar}
          label={task.starred ? '取消收藏' : '收藏'}
          active={task.starred}
          onClick={() => void updateTask(task.id, { starred: !task.starred })}
        />
        <IconButton
          icon={IconTimer}
          label="开始专注 25 分钟"
          onClick={() => {
            startFocus(task.id, 25)
            toast('已开始 25 分钟专注')
            // 顺手打开专注面板，让计时可见——否则开始后界面毫无反馈。
            window.dispatchEvent(new CustomEvent('shenshi:focus'))
          }}
        />
        <div className="relative">
          <IconButton icon={IconMore} label="更多" onClick={() => setMoreOpen((v) => !v)} />
          <Popover open={moreOpen} onClose={() => setMoreOpen(false)} align="right" width={190}>
            <button
              type="button"
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
              onClick={async () => {
                await updateTask(task.id, { important: !task.important, urgent: !task.urgent })
                setMoreOpen(false)
              }}
            >
              <IconSparkle size={13} className="text-ink-3" />
              {task.important && task.urgent ? '取消重要与紧急' : '标为重要且紧急'}
            </button>
            <button
              type="button"
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
              onClick={async () => {
                await toggleTask(task.id)
                setMoreOpen(false)
              }}
            >
              <IconCheck size={13} className="text-ink-3" />
              {done ? '标记为未完成' : '标记为已完成'}
            </button>
            {task.repeatRule && !done ? (
              <button
                type="button"
                className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
                onClick={async () => {
                  setMoreOpen(false)
                  await skipTask(task.id)
                }}
              >
                <IconRepeat size={13} className="text-ink-3" />
                跳过本次
              </button>
            ) : null}
            <button
              type="button"
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
              onClick={async () => {
                setMoreOpen(false)
                const copy = await duplicateTask(task.id)
                if (copy) setSelectedTask(copy.id)
              }}
            >
              <IconCopy size={13} className="text-ink-3" />
              复制一份
            </button>
            <button
              type="button"
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
              onClick={async () => {
                setMoreOpen(false)
                const updated = await updateTask(task.id, { archived: !task.archived })
                if (updated) {
                  toast(task.archived ? '已恢复到日常视图' : '已归档，侧栏「已归档」可找回')
                  // 归档会改变任务在哪些视图出现，关掉详情让列表刷新后的口径说话。
                  onClose()
                }
              }}
            >
              <IconArchive size={13} className="text-ink-3" />
              {task.archived ? '取消归档' : '归档任务'}
            </button>
            <div className="my-1 border-t border-line" />
            <button
              type="button"
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-p-high hover:bg-p-high/10"
              onClick={async () => {
                setMoreOpen(false)
                const ok = await confirm({
                  title: `删除「${task.title}」`,
                  message: '删除后可在左下角撤销，超过 10 分钟才彻底消失。',
                  confirmText: '删除',
                  danger: true,
                })
                if (ok) {
                  await deleteTask(task.id)
                  onClose()
                }
              }}
            >
              <IconTrash size={13} />
              删除任务
            </button>
          </Popover>
        </div>
        <IconButton icon={IconX} label="关闭详情" onClick={onClose} />
      </header>

      {/* 依赖阻塞提示：先完成前置任务，再动这一件 */}
      {blockers.length > 0 ? (
        <div className="flex items-start gap-2 border-b border-p-mid/30 bg-p-mid/8 px-3 py-1.5 text-[0.71875rem] text-p-mid">
          <IconAlert size={13} className="mt-0.5 shrink-0" />
          <span className="min-w-0">
            被「{blockers.map((b) => b.title).join('、')}」挡着 —— 先完成前置任务再开工
          </span>
        </div>
      ) : null}

      <div className="flex-1 overflow-y-auto px-4 pb-8 pt-3">
        {/* 标题 */}
        <textarea
          value={title}
          onChange={(e) => {
            setTitle(e.target.value)
            commitTitle(e.target.value)
          }}
          rows={2}
          placeholder="任务标题"
          className={cx(
            'w-full resize-none bg-transparent text-[0.96875rem] font-medium leading-6 outline-none placeholder:text-ink-3',
            done && 'text-ink-3 line-through',
          )}
        />

        {/* 子任务 */}
        <div className="mt-4">
          <div className="mb-1.5 flex items-center gap-2 text-[0.71875rem] font-medium tracking-wide text-ink-3">
            <IconSubtask size={13} />
            <span>子任务</span>
            {subTotal > 0 ? (
              <>
                <span className="tabular-nums">
                  {subDone}/{subTotal}
                </span>
                <span className="ml-1 h-1 flex-1 overflow-hidden rounded-full bg-line">
                  <span
                    className="block h-full rounded-full bg-jade transition-[width] duration-300"
                    style={{ width: `${(subDone / subTotal) * 100}%` }}
                  />
                </span>
              </>
            ) : null}
          </div>

          <div className="space-y-0.5">
            {task.subtasks.map((s) => (
              <SubtaskItem key={s.id} sub={s} depth={0} />
            ))}
          </div>

          <form
            className="mt-0.5 flex items-center gap-2 rounded-lg px-1 py-1 hover:bg-surface-2"
            onSubmit={async (e) => {
              e.preventDefault()
              const v = subInput.trim()
              if (!v) return
              setSubInput('')
              await addSubtask(task.id, v)
            }}
          >
            <IconPlus size={14} className="shrink-0 text-ink-3" />
            <input
              {...compositionProps}
              value={subInput}
              onChange={(e) => setSubInput(e.target.value)}
              onKeyDown={(e) => {
                // 输入法选词的 Enter 属于组合过程，必须阻止默认行为，否则会提前提交表单。
                if (isComposing(e)) e.preventDefault()
              }}
              placeholder="添加子任务"
              className="min-w-0 flex-1 bg-transparent text-[0.8125rem] outline-none placeholder:text-ink-3"
            />
          </form>
        </div>

        {/* 属性区 */}
        <div className="mt-4 space-y-2.5 border-t border-line pt-4">
          {/* 状态：未开始 / 进行中 / 已完成 三态。进行中是执行层的表态。 */}
          <Row label="状态" icon={IconSparkle}>
            <div className="flex gap-1">
              {(
                [
                  { v: 'todo', label: '未开始' },
                  { v: 'in_progress', label: '进行中' },
                  { v: 'done', label: '已完成' },
                ] as const
              ).map((s) => (
                <button
                  key={s.v}
                  type="button"
                  onClick={() => {
                    // 完成走 toggle：重复任务在服务端统一续期，也会给出「下一次安排」反馈；
                    // 恢复（含改为进行中）仍走 PATCH。
                    if (s.v === 'done' && task.status !== 'done') void toggleTask(task.id)
                    else if (s.v !== task.status) void updateTask(task.id, { status: s.v })
                  }}
                  className={cx(
                    'rounded-lg border px-2.5 py-1 text-[0.78125rem] transition-colors',
                    s.v === 'in_progress'
                      ? task.status === s.v
                        ? 'border-transparent bg-jade/15 font-medium text-jade'
                        : 'border-line text-ink-2 hover:bg-surface-2'
                      : task.status === s.v
                        ? 'border-transparent font-medium text-seal bg-seal/10'
                        : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                >
                  {s.label}
                </button>
              ))}
            </div>
          </Row>

          {/* 预计时长：做事前先掂量分量，专注计时记录的则是事后实际值 */}
          <Row label="预计" icon={IconClock}>
            <div className="flex flex-wrap items-center gap-1.5">
              {[15, 30, 45, 60, 120].map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => void updateTask(task.id, { estimateMinutes: task.estimateMinutes === m ? 0 : m })}
                  className={cx(
                    'rounded-lg border px-2 py-1 text-[0.75rem] transition-colors',
                    task.estimateMinutes === m
                      ? 'border-seal/45 bg-seal/10 font-medium text-seal'
                      : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                >
                  {m}分
                </button>
              ))}
              <input
                type="number"
                min={0}
                max={1440}
                step={5}
                value={estimateDraft ?? (task.estimateMinutes || '')}
                onChange={(e) => {
                  const v = Math.max(0, Number(e.target.value) || 0)
                  setEstimateDraft(v)
                  commitEstimate(v)
                }}
                onBlur={() => setEstimateDraft(null)}
                placeholder="自定义"
                title="预计时长（分钟）"
                className="w-16 rounded-md border border-line bg-surface px-1.5 py-1 text-[0.75rem] tabular-nums outline-none focus:border-seal/50"
              />
              <span className="text-[0.71875rem] text-ink-3">分钟</span>
            </div>
          </Row>

          {/* 进度：无子任务也能标百分比；有子任务时与其完成度互为印证 */}
          <Row label="进度" icon={IconFlag}>
            <div className="flex items-center gap-2">
              <input
                type="range"
                min={0}
                max={100}
                step={5}
                value={progressDraft ?? task.progress}
                onChange={(e) => {
                  const v = Number(e.target.value)
                  setProgressDraft(v)
                  commitProgress(v)
                }}
                onPointerUp={() => setProgressDraft(null)}
                onKeyUp={() => setProgressDraft(null)}
                className="h-1 flex-1 accent-[var(--jade)]"
              />
              <span className="w-9 text-right text-[0.75rem] tabular-nums text-ink-2">{progressDraft ?? task.progress}%</span>
            </div>
            {subTotal > 0 ? (
              <p className="mt-0.5 text-[0.6875rem] text-ink-3">
                子任务完成度 {Math.round((subDone / subTotal) * 100)}%，可作参照
              </p>
            ) : null}
          </Row>

          {/* 开始日期：只表明「打算从哪天动手」，不参与逾期判定 */}
          <Row label="开始" icon={IconCalendarRange}>
            <div className="flex flex-wrap items-center gap-1.5">
              <input
                type="date"
                className={inputClass}
                value={task.startDate ?? ''}
                onChange={(e) => void updateTask(task.id, { startDate: e.target.value || null })}
              />
              {task.startDate ? (
                <>
                  <span className="text-[0.71875rem] text-ink-3">
                    {task.startDate <= todayStr() ? '已到动手日' : `还有 ${dayDiff(task.startDate, todayStr())} 天`}
                  </span>
                  <button
                    type="button"
                    onClick={() => void updateTask(task.id, { startDate: null })}
                    className="rounded-md border border-line px-2 py-1 text-[0.75rem] text-ink-2 transition-colors hover:border-p-high/40 hover:text-p-high"
                  >
                    清除
                  </button>
                </>
              ) : (
                <span className="text-[0.71875rem] text-ink-3">未定动手日</span>
              )}
            </div>
          </Row>

          {/* 日期与时间 */}
          <div className="relative">
            <Row label="日期" icon={IconCalendar}>
              <div className="flex flex-wrap items-center gap-1.5">
                <QuickDateButtons task={task} onPick={(d) => void setDue(d)} />
                <button
                  type="button"
                  onClick={() => setDatePopover((v) => !v)}
                  className={cx(
                    'inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 text-[0.78125rem] transition-colors',
                    task.dueDate ? 'border-line-strong text-ink' : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                >
                  <IconClock size={12} />
                  {task.dueDate ? `${fullDate(task.dueDate)}${task.dueTime ? ` ${task.dueTime}` : ''}` : '选择日期'}
                </button>
              </div>
              <Popover open={datePopover} onClose={() => setDatePopover(false)} align="left" width={252} side="top">
                <div className="space-y-2.5 p-1">
                  <div className="flex gap-2">
                    <input
                      type="date"
                      className={inputClass}
                      value={task.dueDate ?? ''}
                      onChange={(e) => void setDue(e.target.value || null)}
                    />
                    <input
                      type="time"
                      className={inputClass}
                      value={task.dueTime ?? ''}
                      onChange={(e) => void setDue(task.dueDate ?? todayStr(), e.target.value || null)}
                    />
                  </div>
                  <div className="flex items-center justify-between text-[0.75rem] text-ink-3">
                    <span>结束时间</span>
                    <input
                      type="time"
                      className="rounded-md border border-line bg-surface px-2 py-1 text-[0.78125rem]"
                      value={task.endTime ?? ''}
                      onChange={(e) => void updateTask(task.id, { endTime: e.target.value || null })}
                    />
                  </div>
                  <div className="flex flex-wrap gap-1.5 border-t border-line pt-2">
                    {[
                      ['今天', todayStr()],
                      ['明天', addDays(todayStr(), 1)],
                      ['后天', addDays(todayStr(), 2)],
                      ['周日', nextWeekday(0)],
                      ['下周一', nextWeekday(1)],
                      ['一个月后', addMonths(todayStr(), 1)],
                    ].map(([label, date]) => (
                      <button
                        key={label}
                        type="button"
                        onClick={() => void setDue(date)}
                        className="rounded-md border border-line px-2 py-1 text-[0.75rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal"
                      >
                        {label}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => void setDue(null, null)}
                      className="rounded-md border border-line px-2 py-1 text-[0.75rem] text-ink-2 transition-colors hover:border-p-high/40 hover:text-p-high"
                    >
                      清除
                    </button>
                  </div>
                </div>
              </Popover>
            </Row>
          </div>

          {/* 链接：会议地址、工单、文档，与附件分开存放 */}
          <Row label="链接" icon={IconLink}>
            <UrlField value={task.url} onSave={(url) => void updateTask(task.id, { url })} />
          </Row>

          {/* 提醒 */}
          <div className="relative">
            <Row label="提醒" icon={IconBell}>
              <button
                type="button"
                disabled={!task.dueDate}
                onClick={() => setRemindPopover((v) => !v)}
                className={cx(
                  'inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 text-[0.78125rem] transition-colors',
                  task.dueDate ? 'border-line-strong text-ink hover:bg-surface-2' : 'border-line text-ink-3',
                )}
              >
                {!task.dueDate
                  ? '需先设置日期'
                  : task.reminders.length === 0
                    ? '不提醒'
                    : task.reminders.map((r) => (r === 0 ? '准点' : `${r >= 1440 ? r / 1440 + '天' : r >= 60 ? r / 60 + '小时' : r + '分'}前`)).join('、')}
              </button>
              <Popover open={remindPopover} onClose={() => setRemindPopover(false)} align="left" width={200} side="top">
                <div className="py-1">
                  {REMINDER_OPTIONS.map((o) => (
                    <button
                      key={o.value}
                      type="button"
                      onClick={() => void toggleReminder(o.value)}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
                    >
                      <span
                        className={cx(
                          'grid h-4 w-4 place-items-center rounded-[5px] border',
                          task.reminders.includes(o.value) ? 'border-seal bg-seal text-white' : 'border-line-strong',
                        )}
                      >
                        {task.reminders.includes(o.value) ? <IconCheck size={11} strokeWidth={3} /> : null}
                      </span>
                      {o.label}
                    </button>
                  ))}
                  <p className="border-t border-line px-2.5 pb-1 pt-2 text-[0.6875rem] leading-relaxed text-ink-3">
                    未指定时间时按当天 09:00 起算。提醒需要保持页面打开。
                  </p>
                </div>
              </Popover>
            </Row>
          </div>

          {/* 重复 */}
          <div className="relative">
            <Row label="重复" icon={IconRepeat}>
              <button
                type="button"
                onClick={() => setRepeatPopover((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-line-strong px-2 py-1 text-[0.78125rem] text-ink transition-colors hover:bg-surface-2"
              >
                {describeRepeat(task.repeatRule)}
              </button>
              <Popover open={repeatPopover} onClose={() => setRepeatPopover(false)} align="left" width={216} side="top">
                <div className="max-h-72 overflow-y-auto py-1">
                  {repeatOptions.map((o) => (
                    <button
                      key={o.value || 'none'}
                      type="button"
                      onClick={async () => {
                        await updateTask(task.id, { repeatRule: o.value || null })
                        setRepeatPopover(false)
                      }}
                      className={cx(
                        'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] transition-colors',
                        (task.repeatRule ?? '') === o.value ? 'bg-seal/10 text-seal' : 'text-ink hover:bg-surface-2',
                      )}
                    >
                      <IconRepeat size={12} className="text-ink-3" />
                      <span className="flex-1">{o.label}</span>
                      <span className="text-[0.65625rem] text-ink-3">{o.group}</span>
                    </button>
                  ))}
                  {task.repeatRule ? (
                    <div className="border-t border-line px-2.5 pb-1 pt-2">
                      <div className="pb-1 text-[0.6875rem] tracking-wide text-ink-3">下一次从哪天算</div>
                      <div className="flex gap-1">
                        {[
                          { v: 'due' as const, l: '从原到期日', h: '节奏固定，不看实际完成时间' },
                          { v: 'done' as const, l: '从完成日', h: '按实际完成时间往后排' },
                        ].map((o) => (
                          <button
                            key={o.v}
                            type="button"
                            title={o.h}
                            onClick={() => void updateTask(task.id, { repeatFrom: o.v })}
                            className={cx(
                              'flex-1 rounded-md border px-2 py-1 text-[0.71875rem] transition-colors',
                              task.repeatFrom === o.v
                                ? 'border-seal/45 bg-seal/10 font-medium text-seal'
                                : 'border-line text-ink-2 hover:bg-surface-2',
                            )}
                          >
                            {o.l}
                          </button>
                        ))}
                      </div>
                      <p className="pt-1.5 text-[0.6875rem] leading-relaxed text-ink-3">
                        {task.repeatFrom === 'done'
                          ? '完成时才排下一次，适合「隔多久做一次」的事。'
                          : '一到日子就推下一个周期，适合固定日子的例事。'}
                      </p>
                    </div>
                  ) : null}
                  {repeatMeta?.ebbinghausOffsets.length ? (
                    <p className="border-t border-line px-2.5 pb-1 pt-2 text-[0.6875rem] leading-relaxed text-ink-3">
                      艾宾浩斯复习间隔（天）：{repeatMeta.ebbinghausOffsets.join(' / ')}
                    </p>
                  ) : null}
                </div>
              </Popover>
            </Row>
          </div>

          {/* 优先级 */}
          <Row label="优先级" icon={IconFlag}>
            <div className="flex gap-1">
              {PRIORITY_OPTIONS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => void updateTask(task.id, { priority: p.value })}
                  className={cx(
                    'rounded-lg border px-2.5 py-1 text-[0.78125rem] transition-colors',
                    task.priority === p.value ? 'border-transparent font-medium' : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                  style={
                    task.priority === p.value
                      ? { color: p.color, background: `color-mix(in oklab, ${p.color} 13%, transparent)` }
                      : undefined
                  }
                >
                  {p.label}
                </button>
              ))}
            </div>
          </Row>

          {/* 四象限 */}
          <Row label="四象限" icon={IconSparkle}>
            <div className="flex gap-1.5">
              {[
                { key: 'important' as const, label: '重要', on: task.important },
                { key: 'urgent' as const, label: '紧急', on: task.urgent },
              ].map((q) => (
                <button
                  key={q.key}
                  type="button"
                  onClick={() => void updateTask(task.id, { [q.key]: !q.on } as never)}
                  className={cx(
                    'rounded-lg border px-2.5 py-1 text-[0.78125rem] transition-colors',
                    q.on ? 'border-seal/45 bg-seal/10 font-medium text-seal' : 'border-line text-ink-2 hover:bg-surface-2',
                  )}
                >
                  {q.label}
                </button>
              ))}
              <span className="self-center text-[0.6875rem] text-ink-3">
                {task.important && task.urgent
                  ? '立即做'
                  : task.important
                    ? '计划做'
                    : task.urgent
                      ? '尽快处理'
                      : '不紧急'}
              </span>
            </div>
          </Row>

          {/* 清单 */}
          <div className="relative">
            <Row label="清单" icon={IconList}>
              <button
                type="button"
                onClick={() => setListPopover((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-line-strong px-2 py-1 text-[0.78125rem] text-ink transition-colors hover:bg-surface-2"
              >
                <span className="h-2 w-2 rounded-full" style={{ background: task.listColor }} />
                {task.listName}
              </button>
              <Popover open={listPopover} onClose={() => setListPopover(false)} align="left" width={200} side="top">
                <div className="max-h-64 overflow-y-auto py-1">
                  {lists.map((l) => (
                    <button
                      key={l.id}
                      type="button"
                      onClick={async () => {
                        await updateTask(task.id, { listId: l.id })
                        setListPopover(false)
                      }}
                      className={cx(
                        'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] transition-colors',
                        l.id === task.listId ? 'bg-seal/10 text-seal' : 'text-ink hover:bg-surface-2',
                      )}
                    >
                      <span className="h-2 w-2 rounded-full" style={{ background: l.color }} />
                      <span className="flex-1 truncate">{l.name}</span>
                      {l.id === task.listId ? <IconCheck size={12} /> : null}
                    </button>
                  ))}
                </div>
              </Popover>
            </Row>
          </div>

          {/* 标签 */}
          <div className="relative">
            <Row label="标签" icon={IconTag}>
              <div className="flex flex-wrap items-center gap-1.5">
                {task.tags.map((t) => (
                  <span
                    key={t.id}
                    className="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[0.75rem]"
                    style={{ color: t.color, borderColor: `color-mix(in oklab, ${t.color} 40%, transparent)` }}
                  >
                    #{t.name}
                    <button type="button" aria-label="移除标签" onClick={() => void toggleTag(t.id)}>
                      <IconX size={11} />
                    </button>
                  </span>
                ))}
                <button
                  type="button"
                  onClick={() => setTagPopover((v) => !v)}
                  className="inline-flex items-center gap-1 rounded-md border border-dashed border-line px-1.5 py-0.5 text-[0.75rem] text-ink-3 transition-colors hover:border-seal/40 hover:text-seal"
                >
                  <IconPlus size={11} />
                  添加
                </button>
              </div>
              <Popover open={tagPopover} onClose={() => setTagPopover(false)} align="left" width={216} side="top">
                <div className="p-1">
                  <input
                    autoFocus
                    {...compositionProps}
                    value={tagInput}
                    onChange={(e) => setTagInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (isComposing(e)) return
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        void addTagByName(tagInput)
                      }
                    }}
                    placeholder="输入标签名，回车新建"
                    className="mb-1 w-full rounded-lg border border-line bg-surface px-2 py-1.5 text-[0.78125rem] outline-none focus:border-seal/60"
                  />
                  <div className="max-h-52 overflow-y-auto">
                    {tagOptions.length === 0 && !tagInput ? (
                      <p className="px-2 py-1.5 text-[0.71875rem] text-ink-3">没有更多可选标签</p>
                    ) : null}
                    {tagOptions.map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        onClick={() => void toggleTag(t.id)}
                        className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
                      >
                        <span className="h-2 w-2 rounded-full" style={{ background: t.color }} />
                        {t.name}
                      </button>
                    ))}
                  </div>
                  {tagInput ? (
                    <button
                      type="button"
                      onClick={() => void addTagByName(tagInput)}
                      className="mt-1 flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-seal hover:bg-seal/8"
                    >
                      <IconPlus size={12} />
                      新建「{tagInput.replace(/^#/, '')}」
                    </button>
                  ) : null}
                </div>
              </Popover>
            </Row>
          </div>
        </div>

        {/* 关联与依赖 */}
        <div className="mt-5 border-t border-line pt-4">
          <div className="mb-1.5 flex items-center gap-2 text-[0.71875rem] font-medium tracking-wide text-ink-3">
            <IconLink size={13} />
            关联与依赖
            {task.links.length > 0 ? <span className="tabular-nums">{task.links.length}</span> : null}
            <button
              type="button"
              onClick={() => setLinkPopover((v) => !v)}
              className="ml-auto rounded px-1.5 py-0.5 text-[0.6875rem] text-seal transition-colors hover:bg-seal/10"
            >
              添加
            </button>
          </div>
          {task.links.length === 0 ? (
            <p className="text-[0.71875rem] leading-relaxed text-ink-3">
              关联相关任务，或声明依赖：被阻塞的事在对方完成前不宜开工。
            </p>
          ) : (
            <ul className="space-y-1">
              {task.links.map((l) => {
                const ldone = l.status === 'done'
                return (
                  <li
                    key={l.id}
                    className="flex items-center gap-2 rounded-lg border border-line bg-surface-2/50 px-2.5 py-1.5"
                  >
                    <span
                      className={cx(
                        'shrink-0 rounded px-1.5 py-0.5 text-[0.65625rem] font-medium',
                        l.kind === 'blocked_by' && !ldone
                          ? 'bg-p-high/12 text-p-high'
                          : l.kind === 'blocks'
                            ? 'bg-seal/10 text-seal'
                            : 'bg-surface-2 text-ink-3',
                      )}
                    >
                      {l.kind === 'blocked_by' ? '依赖' : l.kind === 'blocks' ? '阻塞着' : '关联'}
                    </span>
                    <span
                      className={cx(
                        'min-w-0 flex-1 truncate text-[0.78125rem]',
                        ldone ? 'text-ink-3 line-through' : 'text-ink',
                      )}
                    >
                      {l.title}
                    </span>
                    {l.kind === 'blocked_by' && !ldone ? (
                      <span className="shrink-0 text-[0.65625rem] text-p-high">未完成</span>
                    ) : ldone ? (
                      <IconCheck size={12} className="shrink-0 text-jade" />
                    ) : null}
                    <button
                      type="button"
                      onClick={() => void removeTaskLink(l.id)}
                      title="移除关联"
                      className="shrink-0 rounded p-0.5 text-ink-3 transition-colors hover:text-p-high"
                    >
                      <IconX size={12} />
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
          <Popover open={linkPopover} onClose={() => setLinkPopover(false)} align="left" width={280} side="top">
            <div className="p-1">
              <input
                autoFocus
                {...compositionProps}
                value={linkQuery}
                onChange={(e) => {
                  setLinkQuery(e.target.value)
                  void searchLinkTargets(e.target.value, task.id)
                }}
                placeholder="搜任务标题，建立关联"
                className="mb-1 w-full rounded-lg border border-line bg-surface px-2 py-1.5 text-[0.78125rem] outline-none focus:border-seal/60"
              />
              <div className="max-h-56 overflow-y-auto">
                {linkResults.map((t) => (
                  <div key={t.id} className="flex items-center gap-1 rounded-lg px-1 hover:bg-surface-2">
                    <button
                      type="button"
                      onClick={async () => {
                        await addTaskLink(task.id, t.id, 'related')
                        setLinkQuery('')
                        setLinkResults([])
                        setLinkPopover(false)
                      }}
                      className="min-w-0 flex-1 truncate py-1.5 text-left text-[0.78125rem] text-ink"
                    >
                      {t.title}
                      <span className="ml-1.5 text-[0.65625rem] text-ink-3">{t.listName}</span>
                    </button>
                    <button
                      type="button"
                      title="设为依赖：它不完成，本任务被阻塞"
                      onClick={async () => {
                        await addTaskLink(task.id, t.id, 'blocked_by')
                        setLinkQuery('')
                        setLinkResults([])
                        setLinkPopover(false)
                      }}
                      className="shrink-0 rounded px-1.5 py-0.5 text-[0.65625rem] text-p-high hover:bg-p-high/10"
                    >
                      依赖
                    </button>
                  </div>
                ))}
                {linkQuery.trim() && linkResults.length === 0 ? (
                  <p className="px-2 py-1.5 text-[0.71875rem] text-ink-3">没有匹配的任务</p>
                ) : null}
              </div>
            </div>
          </Popover>
        </div>

        {/* 备注：支持 Markdown，编辑与预览两态 */}
        <div className="mt-5 border-t border-line pt-4">
          <div className="mb-1.5 flex items-center gap-2 text-[0.71875rem] font-medium tracking-wide text-ink-3">
            <IconNote size={13} />
            备注
            <span className="ml-auto flex items-center gap-0.5">
              {notesMode === 'edit' ? (
                <button
                  type="button"
                  data-notes-checklist
                  onClick={insertChecklist}
                  title="在光标处插入检查清单（- [ ] 行）"
                  className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.6875rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink"
                >
                  <IconCheck size={11} />
                  插入检查清单
                </button>
              ) : null}
              <button
                type="button"
                data-notes-mode="edit"
                onClick={() => setNotesMode('edit')}
                className={cx(
                  'rounded px-1.5 py-0.5 text-[0.6875rem] transition-colors',
                  notesMode === 'edit' ? 'bg-surface-2 text-ink' : 'text-ink-3 hover:text-ink-2',
                )}
              >
                编辑
              </button>
              <button
                type="button"
                data-notes-mode="preview"
                onClick={() => setNotesMode('preview')}
                className={cx(
                  'flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.6875rem] transition-colors',
                  notesMode === 'preview' ? 'bg-surface-2 text-ink' : 'text-ink-3 hover:text-ink-2',
                )}
              >
                <IconEye size={11} />
                预览
              </button>
            </span>
          </div>
          {notesMode === 'edit' ? (
            <textarea
              ref={(el) => {
                notesRef.current = el
                notesInputRef.current = el
              }}
              value={notes}
              data-notes-input
              onChange={(e) => {
                setNotes(e.target.value)
                commitNotes(e.target.value)
              }}
              placeholder="补充背景、链接、验收标准… 支持 Markdown：# 标题、- 列表、**重点**、`代码`"
              className="w-full resize-none rounded-lg border border-line bg-surface-2/50 px-2.5 py-2 text-[0.8125rem] leading-6 outline-none transition-colors placeholder:text-ink-3 focus:border-seal/50 focus:bg-surface"
              rows={3}
            />
          ) : (
            <div
              data-notes-preview
              className="markdown min-h-[68px] rounded-lg border border-line bg-surface-2/40 px-2.5 py-2 text-[0.8125rem] leading-6"
            >
              {notes.trim() ? (
                <div dangerouslySetInnerHTML={{ __html: renderMarkdown(notes) }} />
              ) : (
                <span className="text-ink-3">还没有备注。</span>
              )}
            </div>
          )}
        </div>

        {/* 附件 */}
        <div className="mt-5 border-t border-line pt-4">
          <div className="mb-1.5 flex items-center gap-2 text-[0.71875rem] font-medium tracking-wide text-ink-3">
            <IconPaperclip size={13} />
            附件
            {attachments.length > 0 ? <span className="tabular-nums">{attachments.length}</span> : null}
            <button
              type="button"
              data-attachment-add
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
              className="ml-auto rounded px-1.5 py-0.5 text-[0.6875rem] text-seal transition-colors hover:bg-seal/10 disabled:opacity-50"
            >
              {uploading ? '上传中…' : '添加'}
            </button>
          </div>
          <input
            ref={fileRef}
            type="file"
            multiple
            data-attachment-input
            className="hidden"
            onChange={(e) => void uploadFiles(e.target.files)}
          />
          {attachments.length === 0 ? (
            <p className="text-[0.71875rem] leading-relaxed text-ink-3">
              可附上截图、单据或资料，单个文件最大 32MB。
            </p>
          ) : (
            <ul className="space-y-1">
              {attachments.map((a) => (
                <li
                  key={a.id}
                  data-attachment-row={a.id}
                  className="flex items-center gap-2 rounded-lg border border-line bg-surface-2/50 px-2.5 py-1.5"
                >
                  <IconPaperclip size={12} className="shrink-0 text-ink-3" />
                  <span className="min-w-0 flex-1 truncate text-[0.78125rem] text-ink" title={a.name}>
                    {a.name}
                  </span>
                  <span className="shrink-0 text-[0.6875rem] tabular-nums text-ink-3">{formatSize(a.size)}</span>
                  <a
                    href={api.attachmentURL(a.id)}
                    download={a.name}
                    title="下载"
                    className="shrink-0 rounded p-0.5 text-ink-3 transition-colors hover:text-seal"
                  >
                    <IconDownload size={13} />
                  </a>
                  <button
                    type="button"
                    onClick={() => void removeAttachment(a)}
                    title="删除附件"
                    className="shrink-0 rounded p-0.5 text-ink-3 transition-colors hover:text-p-high"
                  >
                    <IconTrash size={13} />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* 完成度 */}
        {subTotal > 0 ? (
          <div className="mt-5 flex items-center gap-3 rounded-xl border border-line bg-surface-2/50 px-3 py-2.5">
            <ProgressRing value={subDone / subTotal} size={40} stroke={3} color="var(--jade)">
              <span className="text-[0.6875rem] tabular-nums text-ink-2">
                {Math.round((subDone / subTotal) * 100)}%
              </span>
            </ProgressRing>
            <div className="text-[0.75rem] leading-relaxed text-ink-2">
              <div>子任务已完成 {subDone} 项</div>
              <div className="text-ink-3">
                {subDone === subTotal ? '枝节已尽，可以收束了。' : `还剩 ${subTotal - subDone} 项`}
              </div>
            </div>
          </div>
        ) : null}

        <div className="mt-5 flex items-end justify-between gap-3">
          <div className="space-y-0.5 text-[0.6875rem] text-ink-3">
            <div>更新于 {relativeTime(task.updatedAt)}</div>
            {task.dueDate ? <div>计划：{fullDate(task.dueDate)}</div> : null}
          </div>
          <button
            type="button"
            data-save-template
            onClick={() => void saveAsTemplate()}
            title="把这条任务的结构存成模板"
            className="flex shrink-0 items-center gap-1 rounded-lg border border-line px-2 py-1 text-[0.6875rem] text-ink-2 transition-colors hover:border-seal/40 hover:text-seal"
          >
            <IconTemplate size={12} />
            存为模板
          </button>
        </div>
      </div>
    </aside>
  )
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/**
 * 一条子任务：勾选、改名、设日期、设提醒、加子子任务、删除。
 * children 递归渲染成缩进的分解树；日期让子步骤有自己的节律，
 * 提醒走服务端的子任务提醒通道（见 store.DueReminders）。
 */
function SubtaskItem({ sub, depth }: { sub: Subtask; depth: number }) {
  const { addSubtask, updateSubtask, deleteSubtask } = useStore()
  const [childOpen, setChildOpen] = useState(false)
  const [childInput, setChildInput] = useState('')
  const [remindOpen, setRemindOpen] = useState(false)

  const toggleReminder = (value: number) => {
    const has = sub.reminders.includes(value)
    const next = has ? sub.reminders.filter((r) => r !== value) : [...sub.reminders, value].sort((a, b) => a - b)
    void updateSubtask(sub.id, { reminders: next })
  }

  return (
    <div>
      <div
        className={cx(
          'group/sub flex items-center gap-2 rounded-lg px-1 py-1 hover:bg-surface-2',
          depth > 0 && 'ml-4 border-l border-line pl-2',
        )}
      >
        <RoundCheck
          checked={sub.done}
          size={15}
          color="var(--jade)"
          onChange={(next) => void updateSubtask(sub.id, { done: next })}
        />
        <input
          defaultValue={sub.title}
          onBlur={(e) => {
            const v = e.target.value.trim()
            if (v && v !== sub.title) void updateSubtask(sub.id, { title: v })
          }}
          className={cx(
            'min-w-0 flex-1 bg-transparent text-[0.8125rem] outline-none',
            sub.done ? 'text-ink-3 line-through' : 'text-ink',
          )}
        />
        <span className="flex items-center gap-1 opacity-0 transition-opacity group-hover/sub:opacity-100">
          <input
            type="date"
            value={sub.dueDate ?? ''}
            onChange={(e) => void updateSubtask(sub.id, { dueDate: e.target.value || null })}
            title="子任务日期"
            className="w-[7.2rem] rounded-md border border-line bg-surface px-1 py-0.5 text-[0.6875rem] tabular-nums outline-none focus:border-seal/50"
          />
          <span className="relative">
            <IconButton
              icon={IconBell}
              label="子任务提醒"
              size={12}
              active={sub.reminders.length > 0}
              onClick={() => setRemindOpen((v) => !v)}
            />
            <Popover open={remindOpen} onClose={() => setRemindOpen(false)} align="right" width={172} side="top">
              <div className="py-1">
                {REMINDER_OPTIONS.map((o) => (
                  <button
                    key={o.value}
                    type="button"
                    onClick={() => toggleReminder(o.value)}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[0.78125rem] text-ink hover:bg-surface-2"
                  >
                    <span
                      className={cx(
                        'grid h-4 w-4 place-items-center rounded-[5px] border',
                        sub.reminders.includes(o.value) ? 'border-seal bg-seal text-white' : 'border-line-strong',
                      )}
                    >
                      {sub.reminders.includes(o.value) ? <IconCheck size={11} strokeWidth={3} /> : null}
                    </span>
                    {o.label}
                  </button>
                ))}
                <p className="border-t border-line px-2.5 pb-1 pt-2 text-[0.6875rem] leading-relaxed text-ink-3">
                  按子任务日期当天 09:00 起算。
                </p>
              </div>
            </Popover>
          </span>
          {depth === 0 ? (
            <IconButton
              icon={IconPlus}
              label="添加子子任务"
              size={12}
              onClick={() => setChildOpen((v) => !v)}
            />
          ) : null}
          <IconButton icon={IconX} label="删除子任务" size={12} onClick={() => void deleteSubtask(sub.id)} />
        </span>
      </div>

      {/* 子子任务：只展开一层（后端同样限制两级），够拆解「准备答辩 → 订会议室」这类结构 */}
      {depth === 0 && sub.children.length > 0 ? (
        <div className="space-y-0.5">
          {sub.children.map((c) => (
            <SubtaskItem key={c.id} sub={c} depth={1} />
          ))}
        </div>
      ) : null}

      {depth === 0 ? (
        <AddChildSubtask
          show={childOpen}
          value={childInput}
          onChange={setChildInput}
          onClose={() => setChildOpen(false)}
          onSubmit={async () => {
            const v = childInput.trim()
            if (!v) return
            setChildInput('')
            setChildOpen(false)
            await addSubtask(sub.taskId, v, { parentId: sub.id })
          }}
        />
      ) : null}
    </div>
  )
}

/** 「添加子子任务」的行内输入：点 + 号展开，回车提交，失焦或空值时收起。 */
function AddChildSubtask({
  show,
  value,
  onChange,
  onClose,
  onSubmit,
}: {
  show: boolean
  value: string
  onChange: (v: string) => void
  onClose: () => void
  onSubmit: () => Promise<void>
}) {
  const { compositionProps, isComposing } = useIMEGuard()
  if (!show) return null
  return (
    <form
      className="ml-4 flex items-center gap-2 border-l border-line pl-2"
      onSubmit={(e) => {
        e.preventDefault()
        void onSubmit()
      }}
    >
      <IconPlus size={13} className="shrink-0 text-ink-3" />
      <input
        autoFocus
        {...compositionProps}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (isComposing(e)) e.preventDefault()
        }}
        onBlur={() => {
          if (!value.trim()) onClose()
        }}
        placeholder="添加子子任务"
        className="min-w-0 flex-1 bg-transparent py-1 text-[0.78125rem] outline-none placeholder:text-ink-3"
      />
    </form>
  )
}

function Row({
  label,
  icon: Icon,
  children,
}: {
  label: string
  icon: (p: { size?: number; className?: string }) => React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="mt-1 inline-flex w-[62px] shrink-0 items-center gap-1.5 text-[0.71875rem] text-ink-3">
        <Icon size={13} />
        {label}
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

/** 链接输入：失焦或回车才落库，避免每敲一个字符打一次接口。 */
function UrlField({ value, onSave }: { value: string; onSave: (url: string) => void }) {
  const { compositionProps, isComposing } = useIMEGuard()
  const [draft, setDraft] = useState(value)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    if (!editing) setDraft(value)
  }, [value, editing])

  const commit = () => {
    setEditing(false)
    const next = draft.trim()
    if (next !== value) onSave(next)
  }

  if (!editing && !value) {
    return (
      <button
        type="button"
        onClick={() => setEditing(true)}
        className="rounded-lg border border-dashed border-line px-2 py-1 text-[0.75rem] text-ink-3 transition-colors hover:border-seal/40 hover:text-seal"
      >
        添加链接
      </button>
    )
  }

  return (
    <div className="flex items-center gap-1.5">
      <input
        value={draft}
        {...compositionProps}
        autoFocus={editing}
        onChange={(e) => {
          setDraft(e.target.value)
          setEditing(true)
        }}
        onBlur={commit}
        onKeyDown={(e) => {
          if (isComposing(e)) return
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') {
            setDraft(value)
            setEditing(false)
          }
        }}
        placeholder="https://…"
        className={inputClass}
      />
      {value && !editing ? (
        <>
          <a
            href={value}
            target="_blank"
            rel="noreferrer noopener"
            onClick={(e) => e.stopPropagation()}
            className="rounded-md border border-line px-1.5 py-1 text-[0.71875rem] text-seal transition-colors hover:bg-seal/10"
          >
            打开
          </a>
          <button
            type="button"
            onClick={() => {
              setDraft('')
              onSave('')
            }}
            className="rounded-md border border-line px-1.5 py-1 text-[0.71875rem] text-ink-3 transition-colors hover:border-p-high/40 hover:text-p-high"
          >
            清除
          </button>
        </>
      ) : null}
    </div>
  )
}

function QuickDateButtons({ task, onPick }: { task: Task; onPick: (date: string | null) => void }) {
  const today = todayStr()
  const options = [
    { label: '今天', value: today },
    { label: '明天', value: addDays(today, 1) },
    { label: '下周', value: nextWeekday(1) },
  ]
  return (
    <>
      {options.map((o) => (
        <button
          key={o.label}
          type="button"
          onClick={() => onPick(o.value)}
          className={cx(
            'rounded-lg border px-2 py-1 text-[0.78125rem] transition-colors',
            task.dueDate === o.value ? 'border-seal/45 bg-seal/10 text-seal' : 'border-line text-ink-2 hover:bg-surface-2',
          )}
        >
          {o.label}
        </button>
      ))}
    </>
  )
}

function nextWeekday(target: number): string {
  const today = todayStr()
  const curIdx = (weekday(today) - 1 + 7) % 7
  const targetIdx = (target - 1 + 7) % 7
  const delta = (targetIdx - curIdx + 7) % 7 || 7
  return addDays(today, delta)
}
