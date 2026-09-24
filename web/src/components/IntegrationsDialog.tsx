/**
 * 集成与自动化：模板任务、出站 Webhook、自动备份、CalDAV 订阅。
 *
 * 这四件事的共同点是「不参与日常的一屏一屏操作，但决定了能不能放手用」：
 * 模板让重复的事有底稿，Webhook 让外部系统接得上，自动备份让人敢不管，
 * CalDAV 让系统日历看得见。所以放在同一个入口里，不散落在主界面上。
 */

import { useCallback, useEffect, useMemo, useState } from 'react'

import { api } from '../api/client'
import { IconEye, IconPlug, IconPlus, IconTrash } from './icons'
import { Button, Field, Modal, cx, inputClass } from './ui'
import { useStore } from '../store/AppStore'
import type { BackupStatus, Priority, TaskTemplate, Webhook, WebhookDelivery } from '../types'
import { WEBHOOK_EVENTS, WEBHOOK_EVENT_LABEL } from '../types'

type Tab = 'templates' | 'webhooks' | 'backup' | 'caldav'

const TABS: { key: Tab; label: string }[] = [
  { key: 'templates', label: '模板任务' },
  { key: 'webhooks', label: 'Webhook' },
  { key: 'backup', label: '自动备份' },
  { key: 'caldav', label: 'CalDAV' },
]

export function IntegrationsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>('templates')

  return (
    <Modal open={open} onClose={onClose} title="集成与自动化" width={820}>
      <div className="mb-4 flex flex-wrap gap-1 border-b border-line pb-2">
        {TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            data-integration-tab={t.key}
            onClick={() => setTab(t.key)}
            className={cx(
              'rounded-lg px-2.5 py-1 text-[0.78125rem] transition-colors',
              tab === t.key ? 'bg-seal/10 text-seal' : 'text-ink-2 hover:bg-surface-2',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'templates' ? <TemplatesPanel /> : null}
      {tab === 'webhooks' ? <WebhooksPanel /> : null}
      {tab === 'backup' ? <BackupPanel /> : null}
      {tab === 'caldav' ? <CaldavPanel /> : null}
    </Modal>
  )
}

/* ---------------- 模板任务 ---------------- */

function TemplatesPanel() {
  const { lists, toast, confirm } = useStore()
  const [items, setItems] = useState<TaskTemplate[]>([])
  const [draft, setDraft] = useState<TaskTemplate | null>(null)
  const [targetList, setTargetList] = useState<Record<number, number>>({})

  const load = useCallback(async () => {
    try {
      setItems(await api.listTemplates())
    } catch (err) {
      toast(err instanceof Error ? err.message : '读取模板失败', 'error')
    }
  }, [toast])

  useEffect(() => {
    void load()
  }, [load])

  const instantiate = async (t: TaskTemplate) => {
    const listId = targetList[t.id]
    try {
      const task = await api.instantiateTemplate(t.id, listId ? { listId } : {})
      toast(`已生成任务「${task.title}」`)
    } catch (err) {
      toast(err instanceof Error ? err.message : '生成失败', 'error')
    }
  }

  const remove = async (t: TaskTemplate) => {
    const ok = await confirm({
      title: '删除模板',
      message: `将删除模板「${t.name}」，已生成的任务不受影响。`,
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return
    try {
      await api.deleteTemplate(t.id)
      setItems((prev) => prev.filter((x) => x.id !== t.id))
    } catch (err) {
      toast(err instanceof Error ? err.message : '删除失败', 'error')
    }
  }

  return (
    <div className="space-y-3" data-panel="templates">
      <p className="text-[0.71875rem] leading-relaxed text-ink-3">
        把反复要做的事存成底稿，需要时一键铺开成真正的任务。模板只在本地，改动不会影响已生成的任务。
      </p>

      {items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-line px-3 py-6 text-center text-[0.78125rem] text-ink-3">
          还没有模板。可以在任务详情里点「存为模板」，或在这里新建。
        </div>
      ) : (
        <ul className="space-y-2">
          {items.map((t) => (
            <li key={t.id} data-template-row={t.id} className="rounded-xl border border-line bg-surface-2/40 p-3">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[0.8125rem] text-ink">{t.name}</div>
                  <div className="mt-0.5 truncate text-[0.71875rem] text-ink-3">
                    {t.title}
                    {t.dueOffset !== null ? ` · ${dueOffsetLabel(t.dueOffset)}` : ''}
                    {t.subtasks.length > 0 ? ` · ${t.subtasks.length} 项子任务` : ''}
                  </div>
                </div>
                <select
                  value={targetList[t.id] ?? t.listId ?? ''}
                  onChange={(e) => setTargetList((prev) => ({ ...prev, [t.id]: Number(e.target.value) }))}
                  className="shrink-0 rounded-lg border border-line bg-surface px-1.5 py-1 text-[0.71875rem] text-ink-2 outline-none"
                  aria-label="生成到哪个清单"
                >
                  <option value="">默认清单</option>
                  {lists.map((l) => (
                    <option key={l.id} value={l.id}>
                      {l.name}
                    </option>
                  ))}
                </select>
                <Button variant="outline" size="sm" onClick={() => void instantiate(t)}>
                  生成任务
                </Button>
                <button
                  type="button"
                  onClick={() => setDraft(t)}
                  title="编辑"
                  className="shrink-0 rounded p-1 text-ink-3 transition-colors hover:text-ink"
                >
                  <IconEye size={13} />
                </button>
                <button
                  type="button"
                  onClick={() => void remove(t)}
                  title="删除"
                  className="shrink-0 rounded p-1 text-ink-3 transition-colors hover:text-p-high"
                >
                  <IconTrash size={13} />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {draft ? (
        <TemplateEditor
          draft={draft}
          onClose={() => setDraft(null)}
          onSaved={(saved) => {
            setItems((prev) => {
              const idx = prev.findIndex((x) => x.id === saved.id)
              if (idx < 0) return [...prev, saved]
              const next = [...prev]
              next[idx] = saved
              return next
            })
            setDraft(null)
            toast('模板已保存')
          }}
        />
      ) : (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            setDraft({
              id: 0,
              name: '',
              title: '',
              notes: '',
              listId: null,
              priority: 0,
              dueOffset: null,
              dueTime: null,
              reminders: [0],
              repeatRule: null,
              important: false,
              urgent: false,
              tagIds: [],
              subtasks: [],
              sortOrder: 0,
              createdAt: '',
              updatedAt: '',
            })
          }
        >
          <IconPlus size={13} />
          新建模板
        </Button>
      )}
    </div>
  )
}

function TemplateEditor({
  draft,
  onClose,
  onSaved,
}: {
  draft: TaskTemplate
  onClose: () => void
  onSaved: (t: TaskTemplate) => void
}) {
  const { lists, toast } = useStore()
  const [name, setName] = useState(draft.name)
  const [title, setTitle] = useState(draft.title)
  const [notes, setNotes] = useState(draft.notes)
  const [listId, setListId] = useState<number | ''>(draft.listId ?? '')
  const [priority, setPriority] = useState(draft.priority)
  const [dueOffset, setDueOffset] = useState<string>(draft.dueOffset === null ? '' : String(draft.dueOffset))
  const [dueTime, setDueTime] = useState(draft.dueTime ?? '')
  const [subtasks, setSubtasks] = useState(draft.subtasks.join('\n'))
  const [repeatRule, setRepeatRule] = useState(draft.repeatRule ?? '')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!name.trim() || !title.trim()) {
      toast('模板名称与任务标题都要填', 'error')
      return
    }
    setSaving(true)
    try {
      const patch = {
        name: name.trim(),
        title: title.trim(),
        notes,
        listId: listId === '' ? null : Number(listId),
        priority,
        dueOffset: dueOffset.trim() === '' ? null : Number(dueOffset),
        dueTime: dueTime.trim() === '' ? null : dueTime.trim(),
        repeatRule: repeatRule.trim() === '' ? null : repeatRule.trim(),
        subtasks: subtasks
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
      }
      const saved = draft.id
        ? await api.updateTemplate(draft.id, patch)
        : await api.createTemplate(patch)
      onSaved(saved)
    } catch (err) {
      toast(err instanceof Error ? err.message : '保存失败', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-3 rounded-xl border border-line bg-surface-2/40 p-3" data-template-editor>
      <div className="grid gap-2 sm:grid-cols-2">
        <Field label="模板名称">
          <input value={name} onChange={(e) => setName(e.target.value)} className={inputClass} placeholder="每周例会" />
        </Field>
        <Field label="任务标题">
          <input value={title} onChange={(e) => setTitle(e.target.value)} className={inputClass} placeholder="周会前同步本周计划" />
        </Field>
      </div>

      <Field label="备注">
        <textarea
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          rows={2}
          className={cx(inputClass, 'resize-none')}
          placeholder="可选，支持 Markdown"
        />
      </Field>

      <div className="grid gap-2 sm:grid-cols-3">
        <Field label="放入清单">
          <select
            value={listId}
            onChange={(e) => setListId(e.target.value === '' ? '' : Number(e.target.value))}
            className={inputClass}
          >
            <option value="">收集箱</option>
            {lists.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="优先级">
          <select value={priority} onChange={(e) => setPriority(Number(e.target.value) as Priority)} className={inputClass}>
            <option value={0}>无</option>
            <option value={1}>低</option>
            <option value={2}>中</option>
            <option value={3}>高</option>
          </select>
        </Field>
        <Field label="几天后到期" hint="留空则不带日期">
          <input
            value={dueOffset}
            onChange={(e) => setDueOffset(e.target.value)}
            className={inputClass}
            inputMode="numeric"
            placeholder="0 = 今天"
          />
        </Field>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <Field label="时间" hint="HH:MM，留空不限时">
          <input type="time" value={dueTime} onChange={(e) => setDueTime(e.target.value)} className={inputClass} placeholder="09:30" />
        </Field>
        <Field label="重复规则" hint="留空不重复">
          <input value={repeatRule} onChange={(e) => setRepeatRule(e.target.value)} className={inputClass} placeholder="weekly:1" />
        </Field>
      </div>

      <Field label="子任务" hint="一行一条">
        <textarea
          value={subtasks}
          onChange={(e) => setSubtasks(e.target.value)}
          rows={2}
          className={cx(inputClass, 'resize-none')}
          placeholder={'整理议程\n同步进度'}
        />
      </Field>

      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onClose}>
          取消
        </Button>
        <Button size="sm" onClick={() => void save()} disabled={saving}>
          {saving ? '保存中…' : '保存模板'}
        </Button>
      </div>
    </div>
  )
}

function dueOffsetLabel(n: number): string {
  if (n === 0) return '当天到期'
  if (n > 0) return `${n} 天后到期`
  return `${-n} 天前`
}

/* ---------------- Webhook ---------------- */

function WebhooksPanel() {
  const { toast, confirm } = useStore()
  const [items, setItems] = useState<Webhook[]>([])
  const [editing, setEditing] = useState<Webhook | null>(null)
  const [creating, setCreating] = useState(false)
  const [deliveries, setDeliveries] = useState<Record<number, WebhookDelivery[]>>({})

  const load = useCallback(async () => {
    try {
      setItems(await api.listWebhooks())
    } catch (err) {
      toast(err instanceof Error ? err.message : '读取 Webhook 失败', 'error')
    }
  }, [toast])

  useEffect(() => {
    void load()
  }, [load])

  const remove = async (w: Webhook) => {
    const ok = await confirm({
      title: '删除 Webhook',
      message: `将停止向 ${w.url} 推送事件。`,
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return
    try {
      await api.deleteWebhook(w.id)
      setItems((prev) => prev.filter((x) => x.id !== w.id))
    } catch (err) {
      toast(err instanceof Error ? err.message : '删除失败', 'error')
    }
  }

  const toggle = async (w: Webhook) => {
    try {
      const saved = await api.updateWebhook(w.id, { enabled: !w.enabled })
      setItems((prev) => prev.map((x) => (x.id === saved.id ? saved : x)))
    } catch (err) {
      toast(err instanceof Error ? err.message : '更新失败', 'error')
    }
  }

  const test = async (w: Webhook) => {
    try {
      await api.testWebhook(w.id)
      toast('已投递一条测试，可在「最近投递」里看结果')
      // 投递是异步的，稍等一下再看结果。
      window.setTimeout(() => {
        void (async () => {
          try {
            const list = await api.listDeliveries(w.id)
            setDeliveries((prev) => ({ ...prev, [w.id]: list }))
          } catch {
            /* 忽略：下一次点开还会再取 */
          }
        })()
      }, 1200)
    } catch (err) {
      toast(err instanceof Error ? err.message : '测试失败', 'error')
    }
  }

  const showDeliveries = async (w: Webhook) => {
    try {
      const list = await api.listDeliveries(w.id)
      setDeliveries((prev) => ({ ...prev, [w.id]: list }))
    } catch (err) {
      toast(err instanceof Error ? err.message : '读取投递记录失败', 'error')
    }
  }

  return (
    <div className="space-y-3" data-panel="webhooks">
      <p className="text-[0.71875rem] leading-relaxed text-ink-3">
        任务变更时向外部地址推送一条 JSON。签名头 <code>X-Shenshi-Signature</code> 是
        请求体的 HMAC-SHA256（密钥即下面填的签名密钥）。
      </p>

      {items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-line px-3 py-6 text-center text-[0.78125rem] text-ink-3">
          还没有 Webhook。
        </div>
      ) : (
        <ul className="space-y-2">
          {items.map((w) => (
            <li key={w.id} data-webhook-row={w.id} className="rounded-xl border border-line bg-surface-2/40 p-3">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[0.8125rem] text-ink">{w.name || w.url}</div>
                  <div className="mt-0.5 truncate text-[0.71875rem] text-ink-3">{w.url}</div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {w.events.map((e) => (
                      <span key={e} className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.65625rem] text-ink-2">
                        {WEBHOOK_EVENT_LABEL[e] ?? e}
                      </span>
                    ))}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => void toggle(w)}
                  className={cx(
                    'shrink-0 rounded-lg border px-2 py-1 text-[0.6875rem] transition-colors',
                    w.enabled ? 'border-jade/40 text-jade' : 'border-line text-ink-3',
                  )}
                >
                  {w.enabled ? '已启用' : '已停用'}
                </button>
                <Button variant="outline" size="sm" onClick={() => void test(w)}>
                  测试
                </Button>
                <button
                  type="button"
                  onClick={() => setEditing(w)}
                  title="编辑"
                  className="shrink-0 rounded p-1 text-ink-3 transition-colors hover:text-ink"
                >
                  <IconEye size={13} />
                </button>
                <button
                  type="button"
                  onClick={() => void remove(w)}
                  title="删除"
                  className="shrink-0 rounded p-1 text-ink-3 transition-colors hover:text-p-high"
                >
                  <IconTrash size={13} />
                </button>
              </div>

              <div className="mt-2 flex items-center gap-2 text-[0.6875rem] text-ink-3">
                <span>{w.hasSecret ? '已设签名密钥' : '未设签名密钥'}</span>
                <button type="button" onClick={() => void showDeliveries(w)} className="text-seal hover:underline">
                  最近投递
                </button>
              </div>

              {deliveries[w.id] ? (
                <ul className="mt-2 space-y-1 border-t border-line pt-2">
                  {deliveries[w.id]!.slice(0, 5).map((d) => (
                    <li key={d.id} className="flex items-center gap-2 text-[0.6875rem]">
                      <span className={cx('h-1.5 w-1.5 rounded-full', d.ok ? 'bg-jade' : 'bg-p-high')} />
                      <span className="text-ink-2">{WEBHOOK_EVENT_LABEL[d.event] ?? d.event}</span>
                      <span className="tabular-nums text-ink-3">{d.code === 0 ? '未送达' : d.code}</span>
                      {d.error ? <span className="truncate text-ink-3">{d.error}</span> : null}
                      <span className="ml-auto shrink-0 tabular-nums text-ink-3">{d.createdAt.slice(5, 16).replace('T', ' ')}</span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      {editing || creating ? (
        <WebhookEditor
          draft={editing}
          onClose={() => {
            setEditing(null)
            setCreating(false)
          }}
          onSaved={() => {
            setEditing(null)
            setCreating(false)
            void load()
            toast('Webhook 已保存')
          }}
        />
      ) : (
        <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
          <IconPlus size={13} />
          新建 Webhook
        </Button>
      )}
    </div>
  )
}

function WebhookEditor({
  draft,
  onClose,
  onSaved,
}: {
  draft: Webhook | null
  onClose: () => void
  onSaved: () => void
}) {
  const { toast } = useStore()
  const [name, setName] = useState(draft?.name ?? '')
  const [url, setUrl] = useState(draft?.url ?? '')
  const [secret, setSecret] = useState('')
  const [events, setEvents] = useState<string[]>(draft?.events ?? ['task.created', 'task.completed'])
  const [enabled, setEnabled] = useState(draft?.enabled ?? true)
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!url.trim()) {
      toast('回调地址不能为空', 'error')
      return
    }
    setSaving(true)
    try {
      const body = { name: name.trim(), url: url.trim(), events, enabled }
      if (draft) {
        await api.updateWebhook(draft.id, secret ? { ...body, secret } : body)
      } else {
        await api.createWebhook({ ...body, secret })
      }
      onSaved()
    } catch (err) {
      toast(err instanceof Error ? err.message : '保存失败', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-3 rounded-xl border border-line bg-surface-2/40 p-3" data-webhook-editor>
      <Field label="名称" hint="给自己看的备注">
        <input value={name} onChange={(e) => setName(e.target.value)} className={inputClass} placeholder="同步到笔记" />
      </Field>
      <Field label="回调地址" hint="必须是 http:// 或 https:// 开头">
        <input value={url} onChange={(e) => setUrl(e.target.value)} className={inputClass} placeholder="https://example.com/hook" />
      </Field>
      <Field label="签名密钥" hint={draft?.hasSecret ? '留空表示保持不变；填了就覆盖' : '可留空；填了会在请求头带上 HMAC 签名'}>
        <input
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          className={inputClass}
          autoComplete="new-password"
        />
      </Field>
      <Field label="订阅事件">
        <div className="flex flex-wrap gap-2">
          {WEBHOOK_EVENTS.map((e) => (
            <label key={e} className="flex items-center gap-1.5 text-[0.75rem] text-ink-2">
              <input
                type="checkbox"
                className="h-3.5 w-3.5 accent-[var(--seal)]"
                checked={events.includes(e)}
                onChange={(ev) =>
                  setEvents((prev) => (ev.target.checked ? [...prev, e] : prev.filter((x) => x !== e)))
                }
              />
              {WEBHOOK_EVENT_LABEL[e]}
            </label>
          ))}
        </div>
      </Field>
      <label className="flex items-center gap-2 text-[0.78125rem] text-ink-2">
        <input
          type="checkbox"
          className="h-3.5 w-3.5 accent-[var(--seal)]"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        启用
      </label>
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onClose}>
          取消
        </Button>
        <Button size="sm" onClick={() => void save()} disabled={saving}>
          {saving ? '保存中…' : '保存'}
        </Button>
      </div>
    </div>
  )
}

/* ---------------- 自动备份 ---------------- */

function BackupPanel() {
  const { settings, saveSettings, toast } = useStore()
  const [status, setStatus] = useState<BackupStatus | null>(null)
  const [running, setRunning] = useState(false)

  const enabled = settings.autoBackup === '1'
  const hour = settings.autoBackupHour ?? '3'
  const keep = settings.autoBackupKeep ?? '14'

  const load = useCallback(async () => {
    try {
      setStatus(await api.backupStatus())
    } catch (err) {
      toast(err instanceof Error ? err.message : '读取备份状态失败', 'error')
    }
  }, [toast])

  useEffect(() => {
    void load()
  }, [load])

  const run = async () => {
    setRunning(true)
    try {
      const res = await api.runBackup()
      toast(`已备份到 ${res.file.split('/').pop()}`)
      await load()
    } catch (err) {
      toast(err instanceof Error ? err.message : '备份失败', 'error')
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="space-y-4" data-panel="backup">
      <p className="text-[0.71875rem] leading-relaxed text-ink-3">
        每天在设定的时点导出一份完整备份，内容与「导出完整备份 ZIP」一致：JSON 连同任务附件打包，
        单独一份就能还原全部。备份写在服务端数据目录，与数据库同盘——真要防灾还是把整个数据目录另存一份。
      </p>

      <label className="flex items-center gap-2 text-[0.78125rem] text-ink-2">
        <input
          type="checkbox"
          className="h-3.5 w-3.5 accent-[var(--seal)]"
          checked={enabled}
          onChange={(e) => void saveSettings({ autoBackup: e.target.checked ? '1' : '0' })}
        />
        每天自动备份一次
      </label>

      <div className="grid gap-2 sm:grid-cols-2">
        <Field label="备份时间">
          <select
            value={hour}
            onChange={(e) => void saveSettings({ autoBackupHour: e.target.value })}
            className={inputClass}
          >
            {Array.from({ length: 24 }, (_, h) => (
              <option key={h} value={String(h)}>
                {String(h).padStart(2, '0')}:00
              </option>
            ))}
          </select>
        </Field>
        <Field label="保留份数" hint="超出后自动删掉最旧的">
          <select
            value={keep}
            onChange={(e) => void saveSettings({ autoBackupKeep: e.target.value })}
            className={inputClass}
          >
            {['7', '14', '30', '60'].map((n) => (
              <option key={n} value={n}>
                最近 {n} 份
              </option>
            ))}
          </select>
        </Field>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onClick={() => void run()} disabled={running}>
          {running ? '备份中…' : '立即备份一次'}
        </Button>
        {status?.lastAt ? (
          <span className="text-[0.71875rem] text-ink-3">
            上次备份：{status.lastAt.slice(0, 16).replace('T', ' ')}
            {status.lastFile ? ` · ${status.lastFile}` : ''}
          </span>
        ) : (
          <span className="text-[0.71875rem] text-ink-3">还没有自动备份过</span>
        )}
      </div>

      {status?.lastError ? (
        <div className="rounded-lg border border-p-high/30 bg-p-high/8 px-3 py-2 text-[0.71875rem] text-p-high">
          上次备份失败：{status.lastError}
        </div>
      ) : null}

      {status && status.files.length > 0 ? (
        <div className="space-y-1">
          <div className="text-[0.71875rem] font-medium tracking-wide text-ink-3">
            现有备份（{status.files.length} 份）
          </div>
          <ul className="max-h-40 space-y-1 overflow-auto">
            {status.files.map((f) => (
              <li key={f} className="truncate text-[0.71875rem] text-ink-2" title={f}>
                {f}
              </li>
            ))}
          </ul>
          <p className="text-[0.6875rem] text-ink-3">目录：{status.dir}</p>
        </div>
      ) : null}
    </div>
  )
}

/* ---------------- CalDAV ---------------- */

function CaldavPanel() {
  const [copied, setCopied] = useState('')
  // 是否启用了访问口令：决定客户端要不要填密码。
  const [authRequired, setAuthRequired] = useState(false)
  useEffect(() => {
    void (async () => {
      try {
        const s = await api.authStatus()
        setAuthRequired(s.required)
      } catch {
        /* 查不到就当作未启用，提示里不会误导太多 */
      }
    })()
  }, [])

  const base = typeof window === 'undefined' ? '' : window.location.origin
  const principal = `${base}/caldav/user/`
  const calendar = `${base}/caldav/user/calendars/shenshi/`
  const tasks = `${base}/caldav/user/calendars/shenshi-tasks/`

  const copy = async (text: string, key: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(key)
      window.setTimeout(() => setCopied(''), 1600)
    } catch {
      setCopied('')
    }
  }

  const rows = useMemo(
    () => [
      { key: 'principal', label: '账户根地址', value: principal, hint: '在系统日历里添加账户时填这个' },
      { key: 'calendar', label: '日程集合', value: calendar, hint: '有日期的任务，只读' },
      { key: 'tasks', label: '提醒事项集合', value: tasks, hint: '全部任务，可在手机上勾选完成' },
    ],
    [principal, calendar, tasks],
  )

  return (
    <div className="space-y-4" data-panel="caldav">
      <p className="text-[0.71875rem] leading-relaxed text-ink-3">
        用系统自带的日历 / 提醒事项订阅「慎始」的日程与任务。日程集合是只读的；
        提醒事项集合里勾选完成会同步回服务端——方向反过来时，服务端始终是权威。
      </p>

      <div className="space-y-2">
        {rows.map((r) => (
          <div key={r.key} className="rounded-xl border border-line bg-surface-2/40 p-3">
            <div className="flex items-center gap-2">
              <IconPlug size={13} className="shrink-0 text-ink-3" />
              <span className="text-[0.75rem] text-ink-2">{r.label}</span>
              <button
                type="button"
                onClick={() => void copy(r.value, r.key)}
                className={cx(
                  'ml-auto shrink-0 rounded px-1.5 py-0.5 text-[0.6875rem] transition-colors',
                  copied === r.key ? 'text-jade' : 'text-seal hover:bg-seal/10',
                )}
              >
                {copied === r.key ? '已复制' : '复制'}
              </button>
            </div>
            <code className="mt-1 block break-all text-[0.71875rem] text-ink-3">{r.value}</code>
            <div className="mt-0.5 text-[0.6875rem] text-ink-3">{r.hint}</div>
          </div>
        ))}
      </div>

      <div className="rounded-xl border border-line bg-surface-2/40 p-3 text-[0.71875rem] leading-relaxed text-ink-2">
        <div className="mb-1 font-medium text-ink">在 Apple 设备上</div>
        系统设置 → 日历 → 账户 → 添加账户 → 其他 → 添加 CalDAV 账户。
        服务器填账户根地址，用户名任意，<strong>密码填访问口令</strong>（用户名会被忽略）。
        {authRequired
          ? ' 当前服务已启用访问口令。'
          : ' 当前服务未启用访问口令，连接时留空即可——只在可信内网里才该这样。'}
      </div>

      <div className="space-y-1 text-[0.71875rem] leading-relaxed text-ink-3">
        <div>· 勾选提醒事项会写回服务端；在客户端删除或改标题不会被接受。</div>
        <div>· 同步依赖服务端常驻运行，进程退出时系统日历会显示连接失败。</div>
        <div>· 已针对 Apple 客户端做过兼容：不支持的方法返回 403 而非 501。</div>
      </div>
    </div>
  )
}
