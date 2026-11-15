import { useEffect, useState } from 'react';

import { QUOTES } from '../lib/quotes';
import { useStore } from '../store/AppStore';
import { IconCheck, IconTimer } from './icons';
import { Button, ProgressRing, cx, inputClass } from './ui';

/* ============================================================
   专注页（番茄钟）——独立功能视图
   原先是藏在浮层里的 Modal（任务详情 / 侧栏按钮唤起），现提升为
   与日历/看板并列的视图页。计时状态在 store 里全局存活：
   切走再回来、任务详情里快速开始，都不停摆。
   ============================================================ */

const FOCUS_PRESETS = [
  { minutes: 25, label: '25 分钟 · 一番茄' },
  { minutes: 45, label: '45 分钟 · 深度一段' },
  { minutes: 15, label: '15 分钟 · 启动一下' }
];

export function FocusView() {
  const { focus, startFocus, stopFocus, tasks, stats } = useStore();
  const [taskId, setTaskId] = useState<number | ''>('');
  const [minutes, setMinutes] = useState(25);
  const [custom, setCustom] = useState(40);

  useEffect(() => {
    if (focus.running && focus.taskId) setTaskId(focus.taskId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focus.running]);

  const mm = String(Math.floor(focus.remaining / 60)).padStart(2, '0');
  const ss = String(focus.remaining % 60).padStart(2, '0');
  const progress = focus.running ? 1 - focus.remaining / (focus.minutes * 60) : 0;
  const current =
    tasks.find((t) => t.id === focus.taskId) ?? tasks.find((t) => t.id === taskId);
  const openTasks = tasks.filter((t) => t.status !== 'done');

  return (
    <div className="flex h-full justify-center overflow-y-auto px-4 py-8">
      <div className="flex w-full max-w-[26rem] flex-col items-center gap-5">
        <ProgressRing value={progress} size={180} stroke={10}>
          <div className="text-center">
            <div className="brand-serif text-[2rem] leading-none font-semibold tabular-nums text-ink">
              {mm}:{ss}
            </div>
            <div className="mt-1.5 text-[0.6875rem] text-ink-3">
              {focus.running ? `共 ${focus.minutes} 分钟` : '待开始'}
            </div>
          </div>
        </ProgressRing>

        <p className="max-w-[18rem] text-center text-[0.8125rem] leading-relaxed text-ink-2">
          {focus.running
            ? current
              ? `正在专注：${current.title}`
              : '未关联任务，专注时间仍会记录。'
            : '选一段时长、标注一件事，开始一段只属于自己的时间。'}
        </p>

        {focus.running ? (
          <>
            <div className="flex gap-2">
              <Button variant="ghost" onClick={() => void stopFocus(false)}>
                放弃本次
              </Button>
              <Button variant="primary" icon={IconCheck} onClick={() => void stopFocus(true)}>
                提前结束并记录
              </Button>
            </div>
            <p className="text-[0.71875rem] text-ink-3">
              倒计时结束会自动记录时长并提示；切到别的视图计时不停。
            </p>
          </>
        ) : (
          <>
            <div className="w-full space-y-1.5">
              {FOCUS_PRESETS.map((p) => (
                <button
                  key={p.minutes}
                  type="button"
                  onClick={() => setMinutes(p.minutes)}
                  className={cx(
                    'flex w-full items-center justify-between rounded-lg border px-3 py-2 text-[0.8125rem] transition-colors',
                    minutes === p.minutes
                      ? 'border-seal bg-seal/10 text-seal'
                      : 'border-line text-ink-2 hover:bg-surface-2'
                  )}
                >
                  {p.label}
                  {minutes === p.minutes ? <IconCheck size={14} /> : null}
                </button>
              ))}
            </div>

            <label className="flex w-full items-center justify-between gap-2 rounded-lg border border-line px-3 py-2 text-[0.8125rem] text-ink-2">
              <span>自定义时长（5–180 分钟）</span>
              <span className="flex items-center gap-1.5">
                <input
                  type="number"
                  min={5}
                  max={180}
                  value={custom}
                  aria-label="自定义专注分钟数"
                  onChange={(e) => {
                    const v = Number(e.target.value);
                    setCustom(v);
                    if (Number.isFinite(v) && v >= 5 && v <= 180) setMinutes(v);
                  }}
                  className={cx(inputClass, 'w-20 text-right')}
                />
                <span className="text-ink-3">分钟</span>
              </span>
            </label>

            <div className="w-full">
              <span className="mb-1.5 block text-[0.71875rem] font-medium tracking-wide text-ink-3">
                关联任务（可选）
              </span>
              <select
                className={inputClass}
                aria-label="关联任务"
                value={taskId}
                onChange={(e) => setTaskId(e.target.value ? Number(e.target.value) : '')}
              >
                <option value="">不关联</option>
                {openTasks.slice(0, 60).map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.title}
                  </option>
                ))}
              </select>
              {openTasks.length > 60 ? (
                <p className="mt-1 text-[0.6875rem] text-ink-3">
                  仅列出前 60 项未完成任务；先在列表里搜索可缩小范围
                </p>
              ) : null}
            </div>

            <Button
              variant="primary"
              icon={IconTimer}
              onClick={() => startFocus(typeof taskId === 'number' ? taskId : null, minutes)}
            >
              开始专注
            </Button>
          </>
        )}

        <p className="text-[0.75rem] text-ink-3">{QUOTES.focus.text + ' —— ' + QUOTES.focus.source}</p>
        {stats && stats.focusMinutes > 0 ? (
          <p className="text-[0.75rem] text-ink-3">近期已专注 {stats.focusMinutes} 分钟</p>
        ) : null}
      </div>
    </div>
  );
}
