# 「慎始」任务操作专项审查报告（2026-09-24）

> **审查范围**：各页面与空间中与**任务创建 / 编辑 / 属性编辑 / 完成**相关的核心操作。
> **审查维度**：① 逻辑正确性 ② 使用体验 ③ 可用性（提示、反馈、错误处理）④ 无障碍（键盘、焦点、屏幕阅读器、对比度）。
> **方法**：对当前源码逐文件静态走查（标注 `文件:行号`），对比度按 WCAG 2.x 相对亮度公式实测计算，键盘可达性按真实 DOM 结构（`div onClick` / `tabIndex=-1` / `opacity-0`）判定。
> **口径**：**P0** 阻断（数据错误或核心操作不可用）；**P1** 高（核心操作出错 / 关键人群不可达）；**P2** 中（体验与一致性）；**P3** 低（边角打磨）。

**结论摘要**：核心写路径的后端质量较 09-23 复审已有明显改善——`UpdateTask` 已进事务、PATCH `status=done` 已与 `/toggle` 统一续期口径（`store/tasks.go:841-847`）、脏数据校验（`validate.go`）与 `batch` action 白名单（`tasks.go:1270-1274`）均已落地。**但前端存在 3 个 P0/P1 级逻辑缺陷与一套系统性无障碍缺口**：

1. **Esc 键由 3 个互不感知的 window/document 监听器共同处理**，导致"关弹窗连带关详情""嵌套弹窗双双关闭"；
2. **详情面板的标题/备注会被异步回写静默回退**（与项目已确立的 `DraftInput` 约定相冲突）；
3. **列表/分组/标签的管理菜单与全部拖拽操作均不可键盘触发**，这些操作对键盘与触屏用户完全不可达；
4. 对比度实测：焦点环 **1.87:1**（浅）/ **2.16:1**（深），次级文字 `--ink-3` **2.80:1**，输入框与勾选框边界 **1.30:1 / 1.59:1** —— 均低于 WCAG 要求。

按严重度统计：**P0 × 0、P1 × 12、P2 × 31、P3 × 26**（合计 69 条，其中 14 条为跨页面共性问题）。

---

## 一、全局共性问题（跨所有页面）

### G1 · Esc 键多监听器互相冲突 —— P1（逻辑 / 无障碍）

| 位置 | 说明 |
|---|---|
| `web/src/App.tsx:62-73` | 全局 `window.addEventListener('keydown', onKey)`：Esc → 关详情 / 退多选 / 收抽屉 |
| `web/src/components/ui.tsx:190-200` | `Modal` 在 **window** 上监听 Esc，`e.stopPropagation()` 后 `onClose()` |
| `web/src/components/ui.tsx:256-275` | `Popover` 在 **document** 上监听 Esc（document 早于 window 冒泡，故能拦住） |
| `web/src/components/QuickAdd` `TaskViews.tsx:830-834` | 输入框内 Esc 清空并失焦 |

**问题**：`Popover`（document）能拦住 window 监听器，但**两个 window 监听器之间无法互相拦截**——`stopPropagation()` 只阻止事件继续传播到下一个节点，同一节点上的其余监听器仍会全部执行（需要 `stopImmediatePropagation`）。因此：

- 任意弹窗打开时按 Esc：**弹窗关闭 + 同时执行全局动作**。典型可复现路径：打开任务详情 → 「删除任务」→ 确认框弹出 → 按 Esc 取消 → **任务详情也一起关掉了**；
- **嵌套弹窗一次 Esc 双关**：「外观与设置」内打开「集成与自动化」，或任一面板内弹出删除确认，Esc 会把两层一起关闭；
- QuickAdd / 搜索框内按 Esc（意图"清空输入"）会**连带关闭右侧详情或退出多选**（该分支在 `App.tsx` 中位于 `typing` 判断**之前**）。

**建议**：建立单点 Esc 分发（EscScope 栈）：在 `document` 捕获阶段统一监听，维护「当前最上层可关闭者」，只调用栈顶的关闭回调；各组件注册/注销自己的 Esc 处理器而非各自监听 window。同时把 `App.tsx` 的 Esc 分支移到 `typing` 判断之后。

### G2 · Modal 完全没有焦点管理 —— P1（无障碍）

`ui.tsx:203-233`。`role="dialog"` + `aria-modal="true"` 已具备，但：

- **无焦点陷阱**：Tab 可以走出弹窗，进入背后的页面内容（晨省/日省/专注/确认框/外观设置/集成设置全部受影响）；
- **打开时不聚焦**：焦点停留在触发按钮上（见 G3 的具体后果）；
- **关闭后不归还焦点**：面板关闭后焦点落到 `body`，键盘用户须从页首重新 Tab；
- **背景未 `inert`/`aria-hidden`**：读屏仍可遍历弹窗背后的整棵 DOM；
- **无 `aria-labelledby`**：标题是裸 `<h2>`（`ui.tsx:218`），未与对话角色关联，读屏打开时不会朗读标题。

**建议**：`Modal` 内加焦点陷阱（首/末元素循环）、`open` 时聚焦首个可聚焦元素（确认框聚焦"取消"）、关闭时 `restoreFocus` 到打开前的 `document.activeElement`、给 `<h2>` 加 `id` 并以 `aria-labelledby` 关联、打开期间对根容器加 `inert`。

### G3 · 危险确认框不抢焦点，Enter 会重复触发原操作 —— P1（逻辑 / 无障碍）

`web/src/components/Overlays.tsx:826-852`。确认框弹出后不移动焦点，焦点仍在触发它的按钮上（例如行菜单里的「删除」、面板里的「清空」）。此时按 Enter 会**再次命中该按钮**（菜单已关但焦点仍在 DOM 上），表现为反复弹确认框或误执行，而不是"确认"或"取消"。破坏性操作默认焦点应落在**取消**上。

**建议**：与 G2 一并修；确认框默认聚焦"取消"，并把危险按钮置于 Tab 顺序末尾。

### G4 · 焦点指示对比度不足 —— P2（无障碍）

`web/src/index.css:157-161` 定义了全局 `:focus-visible { outline: 2px solid var(--ring); }`，`--ring` = `color-mix(in oklab, var(--seal) 45%, transparent)`（`index.css:31`）。实测（WCAG 相对亮度公式）：

| 主题 | 焦点环合成色 | 相邻背景 | 对比度 | 要求 |
|---|---|---|---|---|
| 浅色 | `#daaea1` | `#faf7f2` | **1.87 : 1** | ≥ 3:1 |
| 深色 | `#6c4134` | `#171513` | **2.16 : 1** | ≥ 3:1 |

即：**焦点环存在但几乎看不出来**，不满足 WCAG 2.2 SC 2.4.11（Focus Appearance）与 1.4.11。此外 `outline-offset: 1px` + 全局 `border-radius: 6px` 会在大元素（列表行、日历格）上画出与元素形状不符的圆角描边。

**建议**：`--ring` 提到 `color-mix(... var(--seal) 85%, transparent)` 或直接用 `var(--seal)` 实色，并在深色主题下改用更亮的强调色；至少保证与相邻底色 3:1。

### G5 · 次级文字色 `--ink-3` 对比度不达标 —— P2（无障碍）

`--ink-3`（浅 `#9c948a` / 深 `#786f66`）被大量用于提示语、计数、标签行、时间戳等**小字号**文本（0.65–0.75rem ≈ 10.5–12px）：

| 主题 | 前景 | 背景 | 对比度 | AA 正文要求 |
|---|---|---|---|---|
| 浅色 | `#9c948a` | `#faf7f2` | **2.80 : 1** | ≥ 4.5:1 |
| 深色 | `#786f66` | `#171513` | **3.78 : 1** | ≥ 4.5:1 |

同类还有：`--p-mid` `#bf8324` on paper = **3.03:1**（用于"被依赖阻塞"提示等）、`--p-low` `#4f7f9e` = **4.04:1**（低优先级文字）、`--jade` `#5f7a66` = **4.41:1**（"进行中"徽章）——均在小字号下不达标。

**建议**：把 `--ink-3` 加深到 ≈ `#6f675e`（浅）/ 提亮到 ≈ `#9a9188`（深）以过 4.5:1；`p-mid`/`p-low`/`jade` 作**文字**用时改用各自的深一档色，作**色块**用时不受此限。

### G6 · 输入框与勾选框边界对比度不足 —— P2（无障碍）

| 元素 | 边界色 | 相邻背景 | 对比度 | 要求（1.4.11 非文本） |
|---|---|---|---|---|
| `inputClass` 边框 | `--line` `#e7e1d7` | `--surface` `#ffffff` | **1.30 : 1** | ≥ 3:1 |
| `RoundCheck` 未勾选边框 | `--line-strong` `#d5ccbe` | `--surface` `#ffffff` | **1.59 : 1** | ≥ 3:1 |

`inputClass` 用 `bg-surface`（白）叠在 `bg-surface` 面板上，**唯一可辨识边界就是这条 1.3:1 的细线**，低视力用户在明亮环境下几乎无法定位输入框——这是全站最高频控件的可达性问题。

**建议**：输入框与勾选框边框改用 `--line-strong` 并再加深（目标 3:1，浅色约 `#a89e8e`），或给输入框加 `bg-surface-2` 以形成面差；勾选框未勾选态改用 `--ink-3` 级别的描边。

### G7 · 缺少无障碍实时播报（aria-live） —— P2（无障碍）

全仓 `aria-live` / `role="status"` / `sr-only` 出现次数为 **0**。以下动态反馈对读屏完全静默：

- Toast 提示（含**所有错误提示**，`Overlays.tsx:856-885`）；
- 提醒到达（`Overlays.tsx:730`）；
- QuickAdd 的识别预览（"标题：…"、chips，`TaskViews.tsx:913-936`）；
- 搜索命中数、筛选命中数（`Toolbar.tsx:454,527`）；
- 批量操作条"已选 N 项"（`TaskViews.tsx:1117`）。

**建议**：Toast 容器加 `role="status" aria-live="polite"`（错误用 `assertive`）；提醒中心加 `aria-live="polite"`；解析预览与命中数加 `aria-live="polite"` 的隐藏文本节点。

### G8 · 按钮组缺少选中态语义 —— P2（无障碍）

以下"分段控件"式按钮组全部只有视觉选中态，无 `aria-pressed` 或 `role="radiogroup"`：

`TaskDetail` 状态（`517-541`）/ 优先级（`836-853`）/ 四象限（`864-875`）/ 重复基准（`800-812`）；`Toolbar` 完成状态·优先级·到期区间（`223-289`）、视图切换（`195-208`）；`BoardView` 分组维度（`158-170`）；`CalendarView` 日/周/月（`200-218`）；`HabitsView` 节奏·星期·标记色（`673-767`）；`StatsView` 区间（`124-136`）；`Sidebar` 明暗·字号（`1613-1650`）；`Overlays` 心情（`530-542`）、专注时长预设（`686-699`）。

**建议**：单选语义用 `role="radiogroup"` + `aria-checked`，开关语义用 `aria-pressed`。

### G9 · 表单控件普遍缺少可访问名称 —— P2（无障碍）

| 控件 | 位置 | 现状 |
|---|---|---|
| 快速添加输入框 | `TaskViews.tsx:778-838` | 仅 `placeholder` |
| 搜索框 | `TaskViews.tsx:1222-1236` | 仅 `placeholder`，无 `role="searchbox"` |
| 子任务标题输入 | `TaskDetail.tsx:1326-1336` | 无 `label`、无 `placeholder` → **可访问名为空** |
| 子子任务输入 | `TaskDetail.tsx:1444-1457` | 仅 `placeholder` |
| 标签名输入 / 关联搜索 | `TaskDetail.tsx:951,1065` | 仅 `placeholder` |
| 进度滑块 | `TaskDetail.tsx:586-600` | 无 `aria-label` / `aria-valuetext` |
| 预计时长数字框 | `TaskDetail.tsx:563-578` | 仅 `title` |
| UrlField | `TaskDetail.tsx:1512-1531` | 仅 `placeholder` |
| 日期/时间 `DraftInput` | `ui.tsx:546-563` | 仅可选 `title` |
| 筛选日期区间 | `Toolbar.tsx:292-304` | 无关联 label |

**建议**：给上述控件补 `aria-label`（语义化中文，如"新任务标题"、"子任务标题"）；`Row` 组件（`TaskDetail.tsx:1462-1480`）改为 `<label>`/`aria-labelledby`，让"状态/进度/预计"等分组名与控件建立关联。

### G10 · 行级操作菜单用 `tabIndex={-1}` 的假按钮，管理功能对键盘完全不可达 —— P1（无障碍）

| 位置 | 内容 |
|---|---|
| `Sidebar.tsx:918-928` | 标签行「更多」（改色 / 重命名 / 删除） |
| `Sidebar.tsx:1260-1270` | 分组行「更多」（新建清单 / 新建子分组 / 重命名 / 折叠 / 归档 / 管理） |
| `Sidebar.tsx:1437-1447` | 清单行「更多」（重命名 / 收藏 / 归档 / 移动到分组） |
| `CalendarView.tsx:388-396` | 日期格「+」（在该日新建任务） |

四处均为 `<span role="button" tabIndex={-1} onClick>`：**不在 Tab 顺序中**，且没有 `onKeyDown`（即使聚焦也不响应 Enter/Space）。叠加 `opacity-0 group-hover:opacity-100`，鼠标用户也要先悬停才看得见。结论：**清单/分组/标签的重命名、归档、删除、移动到分组，以及日历按日新建，对键盘用户完全不存在**。

**建议**：改为真实 `<button>`；可见性策略由 `opacity-0` 改为 `opacity-0 group-hover:opacity-100 focus-visible:opacity-100`（或常驻低透明度）；菜单容器补 `role="menu"` / `menuitem` 与方向键导航。

### G11 · hover-only 控件在获得焦点时依然不可见 —— P1（无障碍）

`opacity-0 ... group-hover:opacity-100` 使控件**在聚焦时也保持透明**（opacity 作用于元素及其轮廓），键盘用户 Tab 到一个看不见的按钮：

- `TaskViews.tsx:288` 任务行内「置顶/收藏/重命名/更多」（4 个控件）；
- `TaskDetail.tsx:1337` 子任务操作条（日期/提醒/加子任务/删除）；
- `HabitsView.tsx:585` 习惯「更多」；
- `Sidebar.tsx:760,776` 保存筛选的「重命名」「删除」；
- `Sidebar.tsx:999,1011,1023` 归档区"恢复"文字提示。

**建议**：所有 hover-only 控件追加 `focus-within:opacity-100` / `focus-visible:opacity-100`，或对 `@media (hover: none)` 与键盘用户常驻显示。

### G12 · 全部拖拽操作只有鼠标路径 —— P1（无障碍 / 可用性）

HTML5 Drag & Drop 无键盘与触屏替代，以下**核心操作对键盘/触屏用户不可达**：

| 操作 | 位置 |
|---|---|
| 跨列改优先级 / 清单 / 日期 / 标签 | `BoardView.tsx:112-149` |
| 象限间移动（改重要/紧急） | `QuadrantView.tsx:78-84` |
| 日历改期、改时刻、拖回全天 | `CalendarView.tsx:120-127,620-645` |
| 清单/分组同层排序、拖入分组、列表内手动排序 | `Sidebar.tsx:91-146`、`TaskViews.tsx:207-223,1049-1070` |

清单"移动到分组"虽有菜单项（`Sidebar.tsx:1489-1500`）作为部分替代，但该菜单本身受 G10 影响仍不可键盘触达，因此**键盘用户无法把清单放进分组**。

**建议**：为每个拖拽目标补键盘等价操作——看板列加"移到此列"菜单、象限加"设为重要/紧急"、日历格加"移到本日"、侧栏加"移动到分组"键盘入口；同时为触屏补长按拖拽（Pointer Events）或"移动"按钮。

### G13 · 任务"打开"不可键盘触发 —— P1（无障碍）

| 位置 | 问题 |
|---|---|
| `TaskViews.tsx:194-205` | 任务行是 `<div onClick>`，无 `tabIndex`/`role`/`onKeyDown` → 键盘无法打开详情 |
| `TaskViews.tsx:274-284` | 标题改名依赖 `onDoubleClick` → 鼠标专属 |
| `TableView.tsx:75-84` | 表格行 `<tr onClick>` 同理 |
| `CalendarView.tsx:237-250` | 无日期抽屉的 chip 是 `<span draggable onClick>` |

**建议**：行容器改为 `role="button" tabIndex={0}` + Enter/Space 处理（或把标题改为真实按钮），双击改名补充 Enter/F2 快捷与「重命名」按钮（已有按钮但受 G11 影响不可见）。

### G14 · 全局搜索无防抖、无请求序号 —— P1（逻辑 / 性能）

`AppStore.tsx:1367-1377` 的 `setKeyword` 直接改 `selection`，进而令 `refreshTasks`（`396-408`）重建并触发 `460-463` 的 effect → **每敲一个字符发一次 `/api/tasks?q=`**，且没有 `AbortController` 或响应序号校验。慢网络下**后到的旧响应会覆盖新结果**（输入 `abc` 可能最终显示 `ab` 的结果）。

同类重复请求：`sortBy` 变化会同时触发 `616-620` 与 `460-463` 两条 effect → 一次排序拉两次。

**建议**：关键词与筛选走 150–250ms 防抖；`refreshTasks` 内加请求序号（或 AbortController），只接受最新一次的结果；合并 `sortBy` 的两条 effect。

---

## 二、按页面 / 空间逐项检查

### 1. 列表视图（智能清单 / 分组 / 清单 / 标签 / 搜索）

**L1 · 新建的任务在多数视图里即刻消失 —— P1（逻辑 + 体验）**
`TaskViews.tsx:730-750` 仅对 `今天`/`明天` 两个智能清单兜底日期；`AppStore.tsx:678` 仅在 `selection.kind === 'list'` 时补 `listId`。因此在这些视图里用顶部输入框建任务：
`分组` / `标签` / `搜索结果` / `本周` / `最近7天` / `逾期` / `高优先级` / `收藏` / `最近修改` / `最近完成` / `已完成`——任务会落到**收集箱**，**当前视图不显示任何新行**。用户只看到一条"已记下「××」"的 toast，第一反应是"没记上"。09-23 报告已指出同类问题，当时只补了两个兜底分支。
**建议**：按视图语义确定归属——分组/清单视图补 `listId`；`本周/最近7天` 兜底到期日；`逾期` 视图禁用顶部快速添加或明确提示"将记入收集箱"。至少要在成功 toast 中告知归属清单。

**L2 · 「完成」批处理实为 toggle，会把已完成的项翻回未完成 —— P1（逻辑）**
`AppStore.tsx:782-796` 的 `batch('complete')` → 后端 `store/tasks.go:1315` 调用 `s.toggleTask(id, true)`，而 `toggleTask`（`tasks.go:1018-1076`）是**翻转**语义：状态已是 done 的项会被改回 `todo`。批量条同时提供「完成」与「恢复」两个按钮，用户预期二者是幂等集合操作。复现：勾选 5 项（其中 1 项已完成）→ 点「完成」→ 那一项被静默恢复为未完成，而提示是"已处理 5 项"。
**建议**：`batch("complete")` 改用幂等的 `UPDATE ... SET status='done' WHERE status<>'done'` + 对未完成项逐个调用 `spawnNextRepeat` 以保持续期口径；或前端在批量条上按状态拆分提示。

**L3 · 多选态下无选中行高亮 —— P2（体验）**
`TaskViews.tsx:199-205` 的选中样式只认 `selectedTaskId`（详情选中），多选勾选只体现在小勾选框上；对比 `TableView.tsx:81` 已有 `bg-seal/6` 行高亮。列表/看板/四象限中大面积勾选时难以核对。
**建议**：`selectedIds.includes(task.id)` 时给行加与表格一致的底色。

**L4 · 行菜单「切换重要 / 紧急」把两个独立属性绑死 —— P2（逻辑）**
`TaskViews.tsx:407-414` 与 `TaskDetail.tsx:344-351` 都是 `{important: !a, urgent: !b}` 同时翻转。详情面板的四象限区（`TaskDetail.tsx:858-886`）本可分别设置，行菜单却做不到，且会静默覆盖用户在详情里做的精细标注。
**建议**：拆成「标为重要」「标为紧急」两项，或改为四象限子菜单。

**L5 · 快速添加：纯标记输入 + Enter 无任何反馈 —— P3（可用性）**
`TaskViews.tsx:719-721` `if (!title) return`。输入 `#工作` 后回车，界面毫无反应（预览行显示"还差一个标题"，但聚焦状态下容易被忽略）。
**建议**：提交失败时给内联错误或 toast 提示"还差一个标题"。

**L6 · 行内改名提交空值静默丢弃 —— P3（可用性）**
`TaskViews.tsx:181-185`：清空标题后失焦 → 既不保存也不提示，标题保持原文。
**建议**：提示"标题不能为空"并恢复。

**L7 · 分组折叠为局部状态且无 `aria-expanded` —— P3**
`TaskViews.tsx:962,1014-1017`：`collapsed` 随组件卸载丢失（切视图/切清单后展开态复位）；折叠按钮未声明展开态。
**建议**：折叠态持久化到设置；补 `aria-expanded`。

**L8 · BatchBar 的清单下拉未按分组组织，窄屏横向溢出 —— P2（可用性 / 响应式）**
`TaskViews.tsx:1115-1175`：`fixed bottom-6 left-1/2` 内 7 个控件（完成/恢复/移到今天/清单 select/置顶/收藏/删除/关闭）用不换行的 `flex`，360px 宽视口必然溢出且无法滚动；清单下拉是扁平的 `lists.map`，与侧栏树形结构不一致，清单多时难以定位。
**建议**：小屏改为两行或"更多"收纳；下拉按分组加 `<optgroup>`。

### 2. 任务详情面板（属性编辑主战场）

**D1 · 标题 / 备注会被异步回写静默回退 —— P1（逻辑，与项目既有约定冲突）**
`TaskDetail.tsx:140-143` 的 effect 在 `task.title` / `task.notes` 变化时**无条件** `setTitle/setNotes` 覆盖本地状态，而 `commitTitle`（`152-154`，450ms）与 `commitNotes`（`156-158`，550ms）是防抖提交。两条路径都会丢字：

1. **打字停顿超过防抖阈值**：输入 `abc` → 停顿 450ms 触发提交 → 继续输入 `d` → 响应回来（`patchLocalTask`）+ `reconcile()` 220ms 后重拉 → effect 把 `title` 重置为 `abc`，**`d` 消失且光标跳位**；
2. **先改标题、随即操作其他字段**：450ms 内点一个优先级按钮 → `updateTask` 响应带的还是**旧标题** → effect 把标题**整段还原**。

这与记忆里已确立的约定（"不要把受控 input 的 value 直接绑到异步回写的远端状态，统一用 `DraftInput`"，`TaskDetail.tsx` 的日期/时间已套用）**直接冲突**——标题与备注是唯二漏网的字段，且恰恰是编辑频率最高的两个。
**建议**：为标题与备注引入"未聚焦才同步远端"的守卫（`DraftInput` 的 `focused` 思路），或把 `TaskDetail` 的本地状态改为"草稿优先、仅在 `draft === null` 时接受远端值"。

**D2 · 清除日期不清时刻，留下脏数据 —— P2（数据一致性）**
`TaskDetail.tsx:160-169` 的 `setDue(null, null)` 会同时清 `dueTime`，但日期输入框的 `onCommit={(v) => void setDue(v || null)}`（`662`）只传 `dueDate`；后端 `UpdateTask`（`tasks.go:718-730`）对 `due_date` 与 `due_time` 也是独立字段，不联动。结果：任务可以是"无到期日但有 09:00"，UI 上日期显示"选择日期"而时间输入框仍有值；此后任意一次"设日期"都会让旧时刻悄然复活。
**建议**：后端在 `due_date` 被清空时一并清 `due_time`；前端 `setDue` 在 `dueDate === null` 时显式带 `dueTime: null`。

**D3 · 选日期时静默补 09:00 —— P3（体验）**
`TaskDetail.tsx:165`：`if (dueDate && dueTime === undefined && !task.dueTime) patch.dueTime = '09:00'`。点「明天」会同时写入 09:00 并显示为"2026-09-25 09:00"，与"只指定日期"的心智不符（右侧快捷按钮与弹层内的快捷按钮行为不一致）。
**建议**：改为只存日期，或在按钮上标明会带 09:00。

**D4 · 打开详情不聚焦、关闭不归还焦点 —— P2（无障碍）**
`TaskDetail.tsx:308-309` 的 `<aside>` 无 `aria-label`、无 `role`；打开时不移动焦点（键盘用户需自行穿越整页），关闭后焦点丢失。
**建议**：打开时聚焦标题 textarea（并全选或置尾），关闭时把焦点还给触发行（App 层记录 `document.activeElement`）。

**D5 · 属性行标签未与控件关联 —— P2（无障碍）**
`TaskDetail.tsx:1462-1480` 的 `Row` 用 `<span>` 承载"状态/预计/进度/开始/日期/链接/提醒/重复/优先级/四象限/清单/标签"。读屏用户在按钮组与输入框之间穿梭时听不到分组语义。
**建议**：`Row` 输出 `<fieldset>`+`<legend>` 或 `role="group"` + `aria-labelledby`。

**D6 · 子任务：操作条 hover-only、标题无标签、删除无确认 —— P2（可用性 + 无障碍）**
`TaskDetail.tsx:1326-1336` 子任务标题 `<input>` 既无 `label` 也无 `placeholder`（**可访问名为空**）；`1337` 的操作条（日期/提醒/加子子任务/删除）是 hover-only（见 G11）；`1387` 删除子任务**无二次确认、无撤销**——与任务删除（有确认 + 10 分钟撤销）口径不一致，误点即丢。
**建议**：补 `aria-label`；操作条常驻或在 focus 时可见；删除子任务加确认或纳入撤销槽位。
另：`1326-1331` 用 `defaultValue` + `onBlur` 提交，不走受控约定，且未加 IME 守卫（同一文件其他输入框都有）——中文输入法下失焦可能提交半截词。

**D7 · 预计时长无法表达"未填" —— P3**
`TaskDetail.tsx:563-573`：`value={estimateDraft ?? (task.estimateMinutes || '')}`，清空输入 → `Number('')` = 0 → 立刻把预计时长写成 0（等价于"未填"但会触发一次多余写请求）。`onBlur={() => setEstimateDraft(null)}`。
**建议**：空串时提交 `0` 或跳过提交，避免无意义 PATCH。

**D8 · blockers 请求随每次对账重发 —— P3（性能）**
`TaskDetail.tsx:87-104` 的 effect 依赖 `[task, version]`，而 `task` 是每次 `tasks` 更新后新建的对象引用 → 每次 220ms 对账都重新请求 `/api/tasks/{id}/blocked`。面板常开时是稳定的额外请求源。
**建议**：依赖改为 `[task?.id, task?.status, version]` 或在 `version` 下加节流。

**D9 · 面板宽度用 px 且两处不一致 —— P3**
`TaskDetail.tsx:309` `w-[356px]`、`216` 占位态 `w-[352px]`。二者不一致（4px 跳动）；且违反项目"尺寸全走 rem"的约定（`fontScale` 放大时面板不随之变宽，正文空间被压缩）。
**建议**：统一为 `w-[22.25rem]` 之类的 rem 值。

**D10 · 「存为模板」位置深、且模板不含子任务细节 —— P3**
`TaskDetail.tsx:1273-1282` 位于面板最底部；`saveAsTemplate`（`287-306`）只把子任务标题（`subtasks: task.subtasks.map(s => s.title)`）写入，**子任务的日期/提醒/层级结构全部丢失**，与 `replaceSubtasks` 的历史问题同源。
**建议**：模板至少保留子任务日期与提醒；入口移到"更多"菜单。

### 3. 看板视图

**B1 · 拖到「无标签 / 无日期」列静默无效 —— P2（体验 / 逻辑）**
`BoardView.tsx:125-134`：`tag-none` 列没有数字后缀，`Number(col.key.slice(4))` 得 `NaN` → `if (Number.isFinite(tagId))` 不成立，直接 `return`；`list` 分组的 `tag-none`、`groupBy==='due'` 的 `none` 列落到 `map[col.key] ?? null`（`147`）尚可。用户把卡片拖进"无标签"列后**卡片弹回原位、无任何提示**。
**建议**：为 `tag-none` 实现"移除全部标签"，或把该列设为不可放置并给出 `cursor: not-allowed` + 提示。

**B2 · 按标签分组时拖入新列只增不减 —— P2（逻辑）**
`BoardView.tsx:129-131`：拖到 `#B` 列只 `tagIds: [...现有, B]`，**不移除来源列的 `#A`** → 任务同时出现在两列，与"拖动卡片可跨列调整"（`156`）的说明矛盾。
**建议**：改为"替换标签"（移除来源列的标签再加目标标签），或在无来源上下文时明确为"追加标签"。

**B3 · `onDragOver` 无条件 setState 导致拖拽期间持续重渲染 —— P2（性能）**
`BoardView.tsx:179-182`、`QuadrantView.tsx:105-108`、`CalendarView.tsx:363-366` 都在 `onDragOver` 里无判断地 `setDropCol/setDropKey/setDropDay`。`dragover` 以高频触发（~60Hz），每次都令整列/整个月历重新渲染，任务多时明显掉帧。
**建议**：`if (dropCol !== col.key) setDropCol(col.key)`（或把高亮态从 React state 挪到 CSS `:hover`/`dragenter` 类名）。

**B4 · 空态判断忽略筛选条件 —— P3**
`BoardView.tsx:230`、`QuadrantView.tsx:160` 用 `tasks.filter(t => t.status !== 'done').length === 0` 判断空态，未套 `matchFilter` → 有筛选把内容全部滤掉时，看板只显示"拖到此处"、四象限干脆不显示空态说明。
**建议**：与列内数据同口径。

### 4. 表格视图

**T1 · 列头排序无方向、无 `aria-sort` —— P2（功能 / 无障碍）**
`TableView.tsx:186-199`：点列头只是 `onSort(target)`，**没有升降序切换**，也没有降序能力；`<th>` 未声明 `aria-sort`。而图标（对勾）暗示"已选中该列排序"，用户会尝试再次点击以反转。
**建议**：加 `sortDir` 状态与升降序切换，`<th>` 补 `aria-sort="ascending|descending"`。

**T2 · 表格缺少 caption 与 `scope="col"` —— P3**
`TableView.tsx:56-68`。读屏以表格模式浏览时缺少表格标题与表头作用域。
**建议**：补 `<caption class="sr-only">` 与 `scope="col"`。

### 5. 四象限视图

**Q1 · 拖拽改写会静默覆盖手工标注 —— P3（逻辑）**
`QuadrantView.tsx:83` 一次写入 `important` + `urgent`。若用户在详情里只标了"重要"，拖到"重要不紧急"会同时把 `urgent` 置 false（本已 false，无碍）；但拖到"重要且紧急"会凭空补上"紧急"。考虑到象限是由这两个字段推导的（`64`），定向覆盖属预期，仅需在拖拽后给出反馈。
**建议**：拖拽成功后 toast 一行"已标为重要且紧急"。

**Q2 · 其余问题同 B3、B4。**

### 6. 日历视图

**C1 · 「还有 N 项」打开的是第 N+1 个任务的详情 —— P2（体验）**
`CalendarView.tsx:428-436`：`onClick={() => onOpen(tasks[visible.length])}` —— 文案承诺"展开更多"，行为却是打开第一个被折叠的任务。
**建议**：改为弹层列全当日任务，或把文案改为"查看第 N 项"。

**C2 · 无日期抽屉静默截断 —— P3**
`CalendarView.tsx:237-250`：`noDate.slice(0, 12)`，标题只显示总数"未排期 N 项"，没有任何"仅显示前 12 项"的提示。
**建议**：补"仅显示前 12 项，去「无日期」清单看全部"或加"更多"。

**C3 · 日历不受侧栏选择影响，且就地新建绕过快速解析 —— P2 / P3（体验 / 一致性）**
`CalendarView.tsx:64-102` 自持数据（注释明确"与侧边栏选择无关"），因此**选中某清单后切到日历仍显示全部任务**；就地新建（`129-156`）直接用 `title` 建任务，`#标签`/`!高` 会原样进标题（`TaskViews.tsx:76` 已记录该缺口）。同时归属清单取决于 `selection.kind === 'list'`，在智能视图下会悄悄落到收集箱（同 L1）。
**建议**：日历顶部显示"全部任务"字样或提供清单范围开关；就地新建复用 `parseQuickAdd`。

**C4 · 关闭按钮为裸 SVG `onClick` —— P1（无障碍）**
`CalendarView.tsx:459`、`810` 的 `<IconX onClick={...}>` 不是按钮：不可键盘操作、无可访问名。与 G10（行级菜单假按钮）、G13（行不可键盘打开）同源，属"看似控件、实为装饰标签"的同一类欠账——它们让"取消新建"这个**退出路径**对键盘用户不存在，故定为 P1。
**建议**：改为 `<IconButton icon={IconX} label="取消新建" />`（组件已存在）。

**C5 · 点击时间轴先清空草稿 —— P3（体验）**
`CalendarView.tsx:607-611` 的 `onSurfaceClick` 无条件 `setDraft('')` → 在别处（同一 `draft` 状态被月/周网格共用）已输入的内容会被清掉。
**建议**：仅当 `adding` 变化时清空。

**C6 · 日历缺少 grid 语义 —— P3（无障碍）**
`CalendarView.tsx:466-497`：月/周网格是 `div` + `grid`，无 `role="grid"/"row"/"gridcell"`，读屏无法按日历惯用方式（上下切周、左右切日）导航。
**建议**：引入 grid 角色与行列索引。

### 7. 习惯视图

**H1 · 热力图产生数百个 Tab 停靠点 —— P2（无障碍 / 可用性）**
`HabitsView.tsx:411-420` 每个习惯渲染 `weeks*7` 个格子按钮（默认 26 周 = **182 个**）。5 个习惯即 910 个 Tab 停靠点，键盘用户实际上无法使用。
**建议**：改用 roving tabindex（每个习惯仅 1 个可 Tab 入口，内部用方向键移动），或格子改为 `tabIndex={-1}` 并把补记/撤销放到行级菜单。

**H2 · 热力图状态仅靠颜色/明度区分 —— P2（无障碍）**
`HabitsView.tsx:636-644`：未达标 `bg-surface-2 ring-1`、非排期 `bg-surface-2/40`、部分达成 `bg-seal/45`、已达标 `bg-seal`（有习惯色时改用习惯色）。"未达标 vs 非排期"在浅色下几无差别；状态信息完全依赖色彩（WCAG 1.4.1）。`data-habit-cell-state` 只服务测试，对读屏不可见。
**建议**：为非达标态加形状/描边区分（如实心 vs 空心），并让 `aria-label` 带上状态词（现在 `title` 只含"已记 N 次"）。

**H3 · 习惯删除用原生 `confirm()` —— P2（一致性）**
`HabitsView.tsx:240`。全站其余删除均走 `store.confirm`（可样式化、非阻塞、文案统一）；此处是唯一例外，且原生弹窗在部分浏览器/iframe 中还会被拦截。
**建议**：改用 `store.confirm`。

**H4 · 打卡无并发/重入保护 —— P2（逻辑）**
`HabitsView.tsx:162-175` 的 `run()` 只 `setBusy(id)`，**不拦截重入**；`RoundCheck`（`521-527`）与热力图格子（`628`）都没接 `disabled={busy}`。连点两下今日勾选框会发出两次请求（一记一撤竞态），热力图连点同理。对比 `MorningPlan.run`（`Overlays.tsx:164-173`）用了 `acting` Set 做守卫——此处缺同样的守卫。
**建议**：`run()` 加 `acting` Set；勾选框与格子传 `disabled={busy}`。

**H5 · 标记色按钮无可读色名、节奏与星期按钮无选中态语义 —— P3（无障碍）**
`HabitsView.tsx:754-767` 的色板按钮 `title={c}` 是十六进制串；`673-717` 的"每天/按周"与星期按钮缺少 `aria-pressed`。
**建议**：给 `PALETTE` 配色名（如"苔绿""赭石"）并补 `aria-pressed`。

### 8. 统计视图

**S1 · 趋势图无文本替代 —— P2（无障碍）**
`StatsView.tsx:151-177`：柱条是装饰性 `<span>`，数据只藏在 `title` 里——`title` 对键盘与读屏均不可靠（触屏亦无悬停）。分布条的数值有文本（`297`），趋势图则完全没有。
**建议**：为图表提供 `role="img"` + `aria-label` 概述，或提供可展开的等效数据表。

**S2 · `statsError` 只在无数据时呈现 —— P3（可用性）**
`StatsView.tsx:45-62`：已有旧数据时刷新失败不提示，用户看到的是过期数字。
**建议**：`statsError && stats` 时在图表区加一条"数据可能已过期，重试"的提示条。

### 9. 侧栏与实体弹窗（创建 / 编辑 / 删除的入口）

**N1 · 已归档任务只取 50 条且无"更多" —— P2（可用性）**
`Sidebar.tsx:511-522`：`api.listTasks({ archived: '1', limit: 50 })`。归档超过 50 条后，**多余的任务在前端没有任何恢复入口**（只能改库）；界面也不说明只列了 50 条。该请求还挂在 `version` 上，每次写操作都会重新拉取。
**建议**：分页或"加载更多"；至少提示"仅显示最近 50 条"。

**N2 · 归档清单的删除是唯一无撤销的批量删除 —— P2（一致性）**
`Sidebar.tsx:1489-1500` 与 `EntityDialog.remove`（`340-359`）都提示"清单中的所有任务会被一并删除，此操作不可撤销"。文案本身是诚实的，但与单任务删除（有 10 分钟撤销槽位）形成强烈落差：**同样是删任务，从清单走就永久消失**。
**建议**：`DELETE /api/lists/{id}` 复用任务删除路径，逐条 `stageUndo`（09-23 报告已提，仍未落地）。

**N3 · 收藏的清单在侧栏出现两次 —— P3（体验）**
`Sidebar.tsx:533-535`：`starredLists` 来自全部存活清单，`rootLists` 是根级存活清单（含被收藏的）→ 同一清单同时出现在「收藏的清单」（`675-693`）与主清单树（`837-853`）。
**建议**：主树中跳过已收藏项，或把「收藏的清单」明确标为"快捷方式"。

**N4 · `EntityDialog` 的 Hook 位于条件返回之后 —— P3（潜在缺陷）**
`Sidebar.tsx:288-294` 连续 5 个 `useState` 后 `if (!draft) return null`，而 `useMemo(parentCandidates)` 在 `299`（条件返回**之后**）。当前依赖父组件 `key={draft ? ... : 'none'}`（`1078`）强制重挂载才没有触发 "Rendered more hooks than during the previous render"，属**侥幸正确**：任何一次去掉 key 的重构都会立刻崩。
**建议**：把 `useMemo` 移到条件返回之前，或把内层表单拆成独立组件。

**N5 · 分组菜单「管理分组…」用了垃圾桶图标 —— P3（一致性）**
`Sidebar.tsx:1317-1326`：`icon={IconTrash} danger` 但行为是打开编辑弹窗（可用于删除）。图标语义误导，危险色亦不必要。
**建议**：改用 `IconSettings`/`IconPencil`，删除动作留在弹窗内（已有）。

**N6 · 视图入口对"当前选择"的处理不一致 —— P2（体验）**
`Sidebar.tsx:647-671`：`quadrant` 先 `select({smart:'all'})` 再切视图；`board`/`table`/`calendar`/`stats` 沿用当前选择；`calendar` 内部又完全忽略选择（C3）。用户点击同一竖排的入口却得到三种不同的作用域。
**建议**：统一策略（建议：视图切换一律保留当前选择，日历顶部显式标注作用域）。

**N7 · 状态栏视图名未覆盖"表格" —— P3**
`Sidebar.tsx:556-567` 的 `viewTitle` 没有 `table` 分支 → 切到表格视图时状态栏显示的是清单/智能清单名，与工具栏大标题「表格」不一致。
**建议**：补 `if (view === 'table') return '表格'`。

**N8 · 颜色候选按十六进制命名 —— P3（无障碍）**
`Sidebar.tsx:437-449`、`930-947` 的色板 `aria-label={c}`（如 `#6b7f6e`）。读屏会念出十六进制串。
**建议**：`PALETTE` 改为 `{value,label}[]`，label 用中文色名。

### 10. 覆盖层（晨省 / 日省 / 专注 / 提醒 / 确认 / Toast）

**O1 · 日省保存失败无任何反馈 —— P2（可用性）**
`Overlays.tsx:478-487` 的 `submit` 只有 `try/finally`，没有 `catch`：`api.saveReview` 抛错时 → **未捕获的 Promise rejection + 弹窗停留 + 无错误提示**，用户会以为已经存上（`busy` 会被 finally 复位，按钮恢复可点）。对比 `MorningPlan.start`（`203-212`）走的是自带 catch 的 `saveSettings`，无此问题。
**建议**：加 `catch` 并 toast 服务端错误信息；成功后再 `onClose()`。

**O2 · 晨省的勾选框语义错位 —— P2（无障碍 / 语义）**
`Overlays.tsx:422` 的 `RoundCheck checked={picked} onChange={onPick}` 未传 `title`，于是沿用默认文案"标记为已完成 / 标记为未完成"（`ui.tsx:111`）。而它实际的语义是"选为今日三件事"。视觉用户靠右侧"· 已列入今日三件事"（`429`）补足，**读屏用户拿到的却是"标记为已完成"**——语义相反。
**建议**：`RoundCheck` 暴露 `title`（已支持），传入"列入今日重点 / 移出今日重点"。

**O3 · 提醒中心的勾选框恒为未选中 —— P2（语义 / 无障碍）**
`Overlays.tsx:761` `<RoundCheck checked={false} onChange={...}/>`：点击后立刻 `dismissReminder` 且永不置为选中，读屏读到的是"标记为已完成，未按下"，而它的真实语义是"完成这项"。`aria-pressed` 恒 false 也不符合开关语义。
**建议**：改为明确的按钮（`IconCheck` + "完成"），或改用 `role="button"` 并给出 `aria-label="完成并关闭提醒"`。

**O4 · 三个固定浮层在窄屏互相遮挡 —— P2（响应式）**
`Overlays.tsx:739-741` 提醒中心 `fixed bottom-4 right-4 w-[330px]`；`TaskViews.tsx:1115` 批量条 `fixed bottom-6 left-1/2`；`Overlays.tsx:73` 专注指示条 `fixed bottom-4 left-4`。360–400px 视口下三者必然重叠；提醒中心还**没有整体关闭/收起之外的退出方式**（无 Esc、无外部点击）。
**建议**：统一浮层堆栈（如底部左侧纵向排列）并为提醒中心补"全部忽略"与 Esc 支持。

**O5 · 「重置提醒」无确认、无错误处理 —— P3（可用性）**
`Sidebar.tsx:1825-1828`：`await api.resetReminders(); toast(...)`——既无二次确认（会把已处理台账清空、让旧事项重新提醒），也无 `try/catch`。
**建议**：加确认与错误提示。

**O6 · 专注面板关联任务只列当前视图前 60 项 —— P3（可用性）**
`Overlays.tsx:712-719`：`tasks.filter(...).slice(0, 60)`，且 `tasks` 只含当前视图数据 → 在看板/日历下无法关联其他清单的任务。
**建议**：改为搜索式选择器（复用 `searchLinkTargets` 的接口）。

**O7 · 日省日期不可切换，与文案不符 —— P3**
`Overlays.tsx:455,464`：`date` 恒为今天，但页脚写"可随时补写或修改"（`506`）。用户无法补写昨天。
**建议**：加日期选择（后端 `PUT /api/reviews` 已支持任意 date）。

### 11. 工具栏与搜索

**TB1 · 视图切换按钮缺少当前项语义 —— P3（无障碍）**
`Toolbar.tsx:195-208`：`title` 提供可访问名（可接受），但未用 `aria-current`/`aria-pressed` 声明当前视图；窄屏下标签文字隐藏（`207`），只剩图标。
**建议**：补 `aria-current="page"`（或 `aria-pressed`）。

**TB2 · 排序入口仅列表/表格可用且未说明 —— P3（体验）**
`Toolbar.tsx:470-507`：`view === 'list' || view === 'table'` 才渲染排序按钮。看板/四象限/日历各有自己的隐式排序（如象限内按到期日+优先级，`QuadrantView.tsx:68-73`）却无任何提示。
**建议**：在其他视图给出"当前排序：到期日优先"的说明。

---

## 三、无障碍专项量化小结

| 检查项 | 现状 | 达标 |
|---|---|---|
| 键盘可达：打开任务（列表/表格/日历抽屉/看板卡片） | ✗ 全为 `div/tr onClick` | ✗ |
| 键盘可达：行级管理菜单（清单/分组/标签/日历新建） | ✗ `tabIndex={-1}` 假按钮 | ✗ |
| 键盘可达：所有拖拽操作（改期/改列/改象限/排序/归组） | ✗ 仅 HTML5 DnD | ✗ |
| 键盘可达：hover-only 控件 | ✗ 聚焦时仍 `opacity:0` | ✗ |
| 焦点管理：弹窗陷阱/初始焦点/焦点归还 | ✗ 三者皆无（`ui.tsx:173-234`） | ✗ |
| 焦点可见：焦点环对比度 | 1.87:1（浅）/ 2.16:1（深） | ✗（需 ≥3:1） |
| 对比度：正文 `--ink-2` | 5.53:1 | ✓ |
| 对比度：次级文字 `--ink-3` | 2.80:1 / 3.78:1 | ✗（需 ≥4.5:1） |
| 对比度：UI 边界（输入框/勾选框） | 1.30:1 / 1.59:1 | ✗（需 ≥3:1） |
| 对比度：主按钮 `seal` 上的 `seal-contrast` | 4.56:1 | ✓（临界） |
| 对比度：危险按钮 `p-high` 上的白字 | 5.03:1 | ✓ |
| `aria-live` 动态播报 | 0 处 | ✗ |
| `aria-pressed`/`radiogroup` 选中态 | 0 处（十余组分段控件） | ✗ |
| 表单可访问名称 | 10 处缺失/退化 | ✗ |
| 语义结构（heading / fieldset / table caption / grid） | 详情面板分区、表格、日历、热力图均缺失 | ✗ |
| 减少动效 `prefers-reduced-motion` | 已处理（`index.css:241-248`） | ✓ |
| 状态色不单独承载信息 | 热力图、趋势图、完成态删除线外的进度色 | ✗ |

`aria-*` / `role` / `tabIndex` 在全前端（约 9.5k 行组件）合计仅 **44 处**，且集中在 `ui.tsx`（7）与 `Sidebar.tsx`（13），说明无障碍是系统性的欠账而非零散疏漏。

---

## 四、优先级汇总

### P0 — 阻断
无。核心写路径（创建/更新/完成/删除）在常规操作下数据正确。

### P1 — 高（应立即修，共 12 条）
| # | 问题 | 位置 | 维度 |
|---|---|---|---|
| 1 | Esc 多监听器冲突：关弹窗连带关详情、嵌套弹窗双关、输入框内 Esc 触发全局动作 | `App.tsx:62-73`、`ui.tsx:190-200,256-275` | 逻辑 / 无障碍 |
| 2 | Modal 无焦点陷阱/初始焦点/焦点归还/`aria-labelledby`，背景未 inert | `ui.tsx:173-234` | 无障碍 |
| 3 | 危险确认框不抢焦点 → Enter 重复触发原操作 | `Overlays.tsx:826-852` | 逻辑 / 无障碍 |
| 4 | 标题/备注被异步回写静默回退（两条丢字路径） | `TaskDetail.tsx:140-143,152-158` | 逻辑 |
| 5 | 批量「完成」是 toggle 语义，会恢复已完成的项 | `AppStore.tsx:782-796`、`store/tasks.go:1315` | 逻辑 |
| 6 | 非清单/非今天明天视图新建任务即刻从视图消失 | `TaskViews.tsx:730-750`、`AppStore.tsx:678` | 逻辑 / 体验 |
| 7 | 搜索无防抖 + 无请求序号，旧响应可覆盖新结果 | `AppStore.tsx:396-408,460-463,1367-1377` | 逻辑 / 性能 |
| 8 | 行级管理菜单为 `tabIndex={-1}` 假按钮（清单/分组/标签/日历新建对键盘不存在） | `Sidebar.tsx:918,1260,1437`、`CalendarView.tsx:388` | 无障碍 |
| 9 | 全部拖拽操作无键盘/触屏替代 | `BoardView`、`QuadrantView`、`CalendarView`、`Sidebar`、`TaskViews` | 无障碍 / 可用性 |
| 10 | hover-only 控件在聚焦时仍不可见 | `TaskViews.tsx:288`、`TaskDetail.tsx:1337`、`HabitsView.tsx:585`、`Sidebar.tsx:760,776` | 无障碍 |
| 11 | 任务/表格行不可键盘打开，改名依赖双击 | `TaskViews.tsx:194-205,274-284`、`TableView.tsx:75-84` | 无障碍 |
| 12 | 日历的关闭/取消入口为裸 `span/svg + onClick`，退出路径对键盘不存在 | `CalendarView.tsx:459,810` | 无障碍 |

### P2 — 中（应排期，共 31 条）
| # | 问题 | 位置 |
|---|---|---|
| 13 | 焦点环对比度 1.87:1 / 2.16:1 | `index.css:31,157-161` |
| 14 | `--ink-3`（及 p-mid/p-low/jade 作文字）对比度不足 | `index.css:18,26,27,21` |
| 15 | 输入框 / 勾选框边界对比度 1.30:1 / 1.59:1 | `ui.tsx:386,119` |
| 16 | 无 `aria-live`：toast、提醒、命中数、解析预览全静默 | `Overlays.tsx:856`、`TaskViews.tsx:913`、`Toolbar.tsx:454` |
| 17 | 十余组分段控件缺 `aria-pressed`/`radiogroup` | 见 G8 清单 |
| 18 | 10 处表单控件缺可访问名称 | 见 G9 清单 |
| 19 | 属性行标签未与控件关联（`Row` 用 span） | `TaskDetail.tsx:1462-1480` |
| 20 | 清除日期不清时刻 → 脏数据 | `TaskDetail.tsx:160-169`、`store/tasks.go:718-730` |
| 21 | 打开详情不聚焦、关闭不归还焦点、`<aside>` 无 `aria-label` | `TaskDetail.tsx:308-309` |
| 22 | 子任务：标题无标签、删除无确认、`defaultValue` 无 IME 守卫 | `TaskDetail.tsx:1326-1337,1387` |
| 23 | 看板拖入「无标签/无日期」列静默无效 | `BoardView.tsx:125-134` |
| 24 | 看板按标签分组拖入只增不减，任务同时出现在多列 | `BoardView.tsx:129-131` |
| 25 | `onDragOver` 无条件 setState → 拖拽期间持续重渲染 | `BoardView.tsx:179`、`QuadrantView.tsx:105`、`CalendarView.tsx:363` |
| 26 | 表格列头无升降序切换、无 `aria-sort` | `TableView.tsx:186-199` |
| 27 | 日历「还有 N 项」打开的是第一个隐藏任务 | `CalendarView.tsx:428-436` |
| 28 | 日历不受侧栏选择影响，就地新建绕过快速解析 | `CalendarView.tsx:64-102,129-156` |
| 29 | 习惯热力图 182 个 Tab 停靠点/习惯 | `HabitsView.tsx:411-420` |
| 30 | 热力图状态仅靠颜色/明度区分 | `HabitsView.tsx:636-644` |
| 31 | 习惯删除用原生 `confirm()` | `HabitsView.tsx:240` |
| 32 | 打卡无并发重入保护（连点竞态） | `HabitsView.tsx:162-175,521` |
| 33 | 日省保存失败无反馈 + 未捕获 rejection | `Overlays.tsx:478-487` |
| 34 | 晨省勾选框语义为"标记为已完成"（实为选重点） | `Overlays.tsx:422` |
| 35 | 提醒中心勾选框恒未选中、无 Esc 与外部点击退出 | `Overlays.tsx:739-766` |
| 36 | 三个固定浮层（提醒/批量条/专注）窄屏互相遮挡 | `Overlays.tsx:73,739-741`、`TaskViews.tsx:1115` |
| 37 | 归档任务只取 50 条、无恢复入口提示 | `Sidebar.tsx:511-522` |
| 38 | 删除清单（含其中任务）无撤销，与单任务删除落差 | `Sidebar.tsx:340-359,1489-1500` |
| 39 | 视图入口对"当前选择"三种策略并存 | `Sidebar.tsx:647-671`、`CalendarView.tsx:64` |
| 40 | 统计趋势图无文本替代 | `StatsView.tsx:151-177` |
| 41 | 多选态下行无选中高亮 | `TaskViews.tsx:199-205` |
| 42 | 行菜单「切换重要/紧急」耦合两个属性 | `TaskViews.tsx:407-414`、`TaskDetail.tsx:344-351` |
| 43 | BatchBar 窄屏横向溢出、清单下拉未按分组组织 | `TaskViews.tsx:1115-1175` |

### P3 — 低（边角打磨，共 26 条）
`TaskViews.tsx`：纯标记输入回车无反馈（`719`）、行内改名空值静默丢弃（`181`）、折叠态不持久且无 `aria-expanded`（`962,1014`）。
`TaskDetail.tsx`：选日期静默补 09:00（`165`）、预计时长无法表达"未填"（`563-573`）、blockers 随对账重发（`87-104`）、面板 px 宽度且两处不一致（`216,309`）、「存为模板」入口过深且模板丢子任务日期/提醒（`287-306,1273`）。
`BoardView.tsx` / `QuadrantView.tsx`：空态判断忽略筛选（`230` / `160`）；象限拖拽后无反馈（`83`）。
`CalendarView.tsx`：抽屉截断无提示（`237`）、点击时间轴清空草稿（`607-611`）、缺 grid 语义（`466-497`）。
`HabitsView.tsx`：色板无可读色名、节奏/星期无 `aria-pressed`（`673-767`）。
`StatsView.tsx`：有旧数据时刷新失败不提示（`45-62`）。
`Sidebar.tsx`：收藏清单重复出现（`533-535`）、`EntityDialog` Hook 顺序隐患（`288-306`）、「管理分组…」用垃圾桶图标（`1317`）、状态栏未覆盖"表格"（`556-567`）、颜色按钮以十六进制命名（`437`）。
`Overlays.tsx`：「重置提醒」无确认/无错误处理（`1825-1828`，实际在 `Sidebar.tsx`）、专注关联任务仅前 60 项（`712`）、日省日期不可切换与文案不符（`455,506`）。
`Toolbar.tsx`：视图按钮缺 `aria-current`（`195-208`）、其他视图无排序说明（`470`）。
`TableView.tsx`：缺 caption 与 `scope="col"`（`56-68`）。

---

## 五、建议修复批次

**第一批（阻断体验与键盘可用性，约 12 项 P1）**
1. 重写 Esc 分发为"最上层优先"的单点机制（G1）→ 顺带解决 G3；
2. `Modal` 补焦点陷阱 / 初始焦点 / 焦点归还 / `inert` / `aria-labelledby`（G2）；
3. 统一把 hover-only 控件改为 `focus-visible:opacity-100`，把 `tabIndex={-1}` 假按钮改为真按钮，行/表格行补键盘打开（G9/G10/G11/G13 → P1 第 8/10/11/12 项）；
4. 修 `TaskDetail` 标题/备注的草稿守卫（D1）；
5. 修批量「完成」的 toggle 语义（L2）；
6. 补非清单视图的新建归属与提示（L1）；
7. 搜索防抖 + 请求序号（G14）。

**第二批（一致性，P2）**
8. 统一焦点环与 `--ink-3` / UI 边界对比度（G4/G5/G6）；
9. Toast / 提醒 / 命中数接 `aria-live`（G7）；分段控件补 `aria-pressed`（G8）；补表单标签（G9）；
10. 清除日期联动清时刻（D2）；子任务标题与删除口径（D6）；
11. 看板拖拽的"替换 vs 追加"与空列反馈（B1/B2）；`onDragOver` 收窄重渲染（B3）；
12. 表格排序方向（T1）；日历「还有 N 项」与就地解析（C1/C3）；
13. 习惯热力图 roving tabindex 与状态非色彩表达（H1/H2）；改回 `store.confirm`（H3）；打卡重入守卫（H4）；
14. 日省错误处理（O1）；晨省与提醒中心的勾选语义（O2/O3）；浮层堆叠（O4）；
15. 归档任务分页（N1）；清单删除纳入撤销（N2）；视图入口策略统一（N6）。

**第三批（无障碍系统性建设）**
16. 为所有拖拽交互提供键盘等价入口（G12）——建议以"移动"菜单/按钮的形式补一张键盘操作矩阵，并补 `role="menu"` / 方向键导航；
17. 语义结构补全：详情面板 `fieldset`/`legend`、日历 `role="grid"`、表格 `caption`/`scope`、图表文本替代；
18. 把上述检查项固化为回归清单（对比度阈值 + 键盘路径 + `aria-live`），避免再次系统性欠账。

---

*报告基于 2026-09-24 的源码快照（任务相关前端 `web/src/components/*.tsx` 与 `web/src/store/AppStore.tsx`、后端 `server/internal/store/{tasks,validate}.go`）。对比度为公式实测值，键盘可达性依据真实 DOM 结构判定。*

---

## 六、修复落地（同日）

三批全部实施完毕，`tsc --noEmit` 零错误、`./scripts/build.sh` 通过。要点与偏差如下。

### 新增的基础设施（第一批）

| 文件 | 作用 |
|---|---|
| `web/src/lib/escStack.ts` | Esc 单点仲裁：只有栈顶层消费按键，一次 Esc 只关一层 |
| `web/src/lib/modalLayer.ts` | 弹层层级计数，供背景 `inert` 判定 |

`Modal` 改为 `createPortal` 到 `body`（背景才能整体 inert 而不连带弹窗），并补焦点陷阱、初始焦点、关闭归还、`aria-labelledby`。`Popover` 新增 `menu` 开关：菜单型浮层打开即聚焦首项、关闭归还触发元素；非菜单型（快速添加候选）保持不动焦点。

### 与报告结论有出入的条目（据实修正）

1. **N2「清单删除无撤销」**：后端 `DeleteList`（`store/org.go:395-439`）**已经**把清单内任务压进撤销槽位，09-23 那次就落地了。真正的缺陷是前端确认框写着「此操作不可撤销」，与后端行为相悖 —— 已改为与单任务删除一致的措辞。
2. **「模板丢子任务日期/提醒」**：模板的子任务是 `[]string`（`model.Template.Subtasks`），保留日期/提醒需要改数据结构与生成逻辑，超出本次修复范围。已把提示文案改为「子任务仅保留标题」，不再留下"完整复制"的错觉。
3. **日历网格语义**：未采用 `role="grid"`（半实现的网格语义会让读屏进入应用模式却得不到方向键支持，比现状更差）。改为 `role="group"` + 每个日期格 `aria-label="X 月 X 日，N 项任务"`，读屏获得的信息更完整。
4. **拖拽的键盘等价入口**：以任务行「更多」菜单为载体补齐 —— 新增「移动到清单」列表，重要/紧急拆成两条独立开关（原先一条「一起翻转」），加上原有的改期/优先级/置顶/收藏，跨清单移动、改期、象限调整都有了键盘入口。

### 顺带修掉的相邻问题

- 任务行菜单补 `role="menu"` / `menuitem` / `separator` 层级与 ↑↓ 导航（原先只是视觉上的菜单）。
- `BatchBar` 窄屏横向溢出：改为限宽 + 可换行，`bottom-6 → bottom-20` 避开底部两个固定浮层；「移到清单」下拉按分组树组织（`optgroup`，递归含子分组）。
- 筛选命中数、提醒中心、解析预览接入 `aria-live`。
- 侧栏「已归档」补 `aria-expanded` 与「仅显示最近 50 条」的截断说明。
- 任务列表分区折叠状态持久化到 `localStorage`（切走视图再回来不再全部展开）。
- 色板按钮的可访问名从十六进制改为中文色名（`lib/palette.ts` 的 `colorName()`）。
- 对比度批次：焦点环 `--ring` 改用 `--seal`（1.87:1 → 4.6:1），`--ink-3` 加深，新增 `--control-line` 用于输入框 / 勾选框 / 按钮边界（1.30:1 → 3:1 以上）。
- 表格排序支持升/降序切换与 `aria-sort`，后端 `orderByFor` 补 4 个反向排序值。
- 批量「完成」改为幂等（新增 `store.completeTask`），不再把已完成的项翻回未完成。

### 后续

回归检查项已固化为 `docs/a11y-regression-checklist.md`（对比度阈值 + 键盘路径 + `aria-live` + 项目约定），改动前端组件后按该清单走查。

---

## 七、CI 首轮失败的复盘（同日第三轮）

首次推送（`a9701e4`）后 CI 连续两轮失败，**两轮原因不同**，且都源于本次改动越过了既有契约。记录在此以免重蹈。

### 第 1 轮：`TestTaskCRUD` —— 后端隐式联动违约 PATCH 三态

- 现象：`server/internal/store` 失败，`tasks_test.go:90`「未传的 dueTime 不应被清掉」。
- 根因：在 `UpdateTask` 里新增了「清 `due_date` 时连带清 `due_time`」。既有测试正是 **PATCH 三态契约**（未传字段一律不动）的守卫；而且该联动**本来就是冗余的** —— `TaskDetail.setDue` 早在清日期分支里显式发了 `dueTime: null`。
- 修复（`0189ca5`）：回退后端隐式联动，改由调用方显式表达；唯一漏网的 `TaskViews.tsx` 任务行菜单「清除日期」补上 `dueTime: null`。
- **原则**：业务联动（清 A 连带清 B）不在后端做隐式实现；契约冲突时先跑 `go test` 暴露，不要改测试去迁就实现。

### 第 2 轮：浏览器 UI 冒烟 —— 改 UI 时忘了看定位器

1. **`Modal` 的 Esc 变成焦点依赖**（`scripts/ui-smoke.mjs:242`）：统一 Esc 到仲裁栈时，Modal 改为在对话框容器上监听 `keydown` —— 只有焦点仍在弹窗内才收得到。脚本点掉晨省里的一行后，那一行随之卸载、焦点落回 `<body>`，此后的 Esc 无人接管，弹窗关不掉，下一次点击被遮罩拦截。
   - 修复：`Modal` 改用 `useEscapeLayer(open, onClose)` 注册仲裁栈（不依赖焦点位置），容器上只保留 Tab 焦点陷阱。
2. **改文案打断了选择器**（`ui-smoke.mjs:451`）：任务行「更多」按钮的 `label` 被改成「更多操作」，`title` 随之变化，而脚本按 `button[title="更多"]` 定位。
   - 修复：`label` 回退为「更多」，另用 `aria-label=\`「任务名」的更多操作\`` 提供逐行唯一的可访问名（`IconButton` 的 `{...rest}` 在 `title`/`aria-label` 之后展开，可单独覆盖任一属性）。
3. **顺带发现**：习惯删除由原生 `confirm()` 改为应用内确认框（本次的一致性改进）后，脚本里 `page.once('dialog')` + 单击一次的旧流程失效，删除不再发生，后续用例被确认框的 `footer` 拦截。
   - 修复：脚本跟进新契约 —— 点菜单项弹确认框 → 断言确认框出现 → 再点确认框的「删除」。测试因此**更强**（多一条断言），而非被削弱。

### 教训

- `./scripts/build.sh` 只编译、**不跑测试**；`tsc --noEmit` 只覆盖前端类型。后端改动必须单独跑 `go test ./internal/...`。
- `scripts/ui-smoke.mjs` 是 **UI 契约的守卫**（按 `title=` / `aria-label=` / `data-*` 定位），且在本地即可完整运行（自起临时实例与端口）。改任何可见文案、标签、结构或交互步数之后，先跑它再推。
- 一次提交同时改前后端时，本地验证要覆盖三条链路：`go test` → `tsc` → `node scripts/ui-smoke.mjs`。三者全绿时 CI 才等价于「已在本地验证过」。
