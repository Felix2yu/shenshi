# 无障碍与任务操作 · 回归清单

> 用途：改动 `web/src` 下的组件后跑一遍。本清单是 2026-09-24 任务操作专项审查
> （见 `docs/review-2026-09-24-task-ops.md`）中暴露的系统性欠账的固化形式 ——
> 一次审查能修掉问题，只有清单能让它们不再回来。

## 0. 机械检查（每次必跑）

```bash
# 后端单测：scripts/build.sh 只编译、不跑测试，跳过这步就只能等 CI 报红
cd server && /opt/homebrew/bin/go test ./internal/...

# 类型：vite 不做类型检查，必须单独跑
cd web && /opt/homebrew/bin/node ./node_modules/typescript/bin/tsc --noEmit -p tsconfig.json

# 构建（前端会被打进二进制，改完 CSS/TS 必须重跑）
NODE_BIN=/opt/homebrew/bin/node GO_BIN=/opt/homebrew/bin/go ./scripts/build.sh

# 浏览器 UI 冒烟：本地可完整运行（自起临时实例与端口），改任何可见文案 / 交互步数后必跑
/opt/homebrew/bin/node scripts/ui-smoke.mjs
```

```bash
# 结构自查：分段控件必须成对出现 aria-pressed，选单必须有序数语义
rg -c 'aria-pressed' web/src/components     # 期望 > 20
rg -n 'onClick' web/src/components/ui.tsx   # 任何裸 div onClick 都要复查
```

## 1. 对比度阈值（实测，不是"看着还行"）

| 用途 | 变量 / 位置 | 最低比值 | 依据 |
|---|---|---|---|
| 正文与非装饰性小字 | `--ink` / `--ink-2` | 4.5:1 | WCAG SC 1.4.3 |
| 次级说明文字（`text-ink-3`） | `--ink-3` | 4.5:1（小字） | SC 1.4.3 |
| UI 边界（输入框、勾选框） | `--control-line` | 3:1 | SC 1.4.11 |
| 焦点指示环 | `--ring`（= `--seal`） | 3:1 | SC 1.4.11 / 2.4.11 |

改色板或主题变量后，用下面的脚本量一遍（Pillow 取色 → 对比度公式）：

```bash
/Users/yufei/.workbuddy/binaries/python/envs/default/bin/python - <<'PY'
def lum(c):
    c = [v / 255 for v in c]
    c = [(v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4) for v in c]
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
def ratio(a, b):
    la, lb = lum(a), lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)
print(round(ratio((117, 109, 100), (250, 249, 247)), 2))  # --ink-3 on --paper（浅色主题）
PY
```

## 2. 键盘路径清单（只用键盘走完，不碰鼠标）

| 场景 | 期望 |
|---|---|
| 打开任意弹窗 | 焦点进入弹窗（危险确认框落在「取消」上）、Tab 在弹窗内循环、Esc 只关最上面一层、关闭后焦点回到触发元素 |
| 打开时背景 | 背景整体 `inert`：不可聚焦、读屏不朗读；提醒中心/专注条/Toast 例外，仍可操作 |
| 快速添加 | `#` `!` `/` 弹候选，↓↑ 选、Enter/Tab 应用、Esc 抑制而不关输入框 |
| 任务行 | Tab 可达；Enter 打开详情；**F2** 进入改名；改名空值给出提示并还原 |
| 任务行「更多」 | Tab 可达；有 `aria-haspopup`/`aria-expanded`；菜单内 ↑↓ 移动焦点；「移动到清单」提供跨清单移动的键盘等价入口 |
| 表格 | 行 Tab 可达、Enter 打开；列头 `aria-sort` 随升/降序切换；`caption` 与 `scope="col"` 在位 |
| 日历 | 日期格有「几月几日、几项任务」的可访问名；「在 X 添加任务」「取消新建」为真按钮 |
| 习惯热力图 | 每个习惯只有 1 个 Tab 停靠点（今天）；网格内 ←→↑↓ 移动；状态有文字说明，不只靠颜色 |
| 拖拽类操作 | 均有非拖拽入口：改期/优先级/重要紧急/移动清单走任务菜单；清单归属走编辑弹窗 |

## 3. 动态播报（`aria-live`）

以下区域变化时必须被读屏播报：

- Toast 容器（`role="status" aria-live="polite"`）
- 提醒中心（提醒是异步出现的）
- 筛选弹窗的「命中 N 项」
- 快速添加的解析预览（标签 / 日期 / 放入哪张清单）
- 统计视图的刷新失败提示

新增任何"操作后异步出现的结果"，先问一句：读屏用户怎么知道它出现了。

## 4. 语义与命名

- 分段控件（状态 / 优先级 / 预计 / 提醒 / 节奏 / 星期 / 日历粒度 / 统计区间 / 筛选三组 / 视图切换）：一律 `aria-pressed` 或 `aria-current`，容器用 `role="group" aria-label`。
- 表单控件：`<label for>` 优先；视觉上无标签的（搜索、日期、时长）必须有 `aria-label`。
- 图标按钮：`aria-label` 要带上下文（「移除标签『工作』」而不是「移除」）。
- 色板按钮：用中文色名（`colorName()`），不要拿十六进制当可访问名。
- 属性行分组：`TaskDetail` 的 `Row` 用 `role="group" aria-labelledby` 关联可见标签。

## 5. 项目约定（改动前先读）

- **受控输入 × 异步回写**：不要给远端状态直接绑原生输入框，用 `DraftInput`。
- **Esc 仲裁**：新增浮层走 `pushEscapeLayer` / `useEscapeLayer`，不要各自监听 `document`。
- **背景 inert**：新弹窗走 `Modal`（已 `createPortal` 到 body 并计数层级）。
- **PATCH 三态**：可清空字段一律 `model.Opt[T]`，裸指针无法区分「未传」与「置 null」。
- **分组是树**：涉及分组/清单的遍历一律递归（`flattenFolders` / `mapFolderInTree` / `reorderFoldersInTree`）。
- **字号单位**：源码禁止 `text-[Npx]`，一律 `text-[Nrem]`，否则绕过 fontScale 缩放。
- **焦点可见**：hover-only 控件必须同时有 `focus-visible` / `focus-within` 可见性；`index.css` 已有全局补丁，新增模式请沿用。
- **Esc 必须不依赖焦点**：`Modal` 也走 `useEscapeLayer` 注册仲裁栈，容器上的 `keydown` 只留 Tab 陷阱。焦点是会被夺走的 —— 被点击的元素一旦随之卸载，焦点就落回 `<body>`，只靠容器监听会漏掉按键（2026-09-24 冒烟实测）。
- **改文案前先看 `scripts/ui-smoke.mjs`**：它按 `title=` / `aria-label=` / `data-*` 定位，是 UI 契约的守卫。改可见文案、图标按钮的 `label`、或交互步数（如把原生 `confirm()` 换成应用内确认框）之后，脚本要同步跟进 —— 且应补断言而非删断言。
- **业务联动由调用方显式表达**：不在后端做「清 A 连带清 B」的隐式联动，否则违约 PATCH 三态契约（既有测试就是该契约的守卫）。
