// Package model 定义「慎始」的核心领域模型。
// 命名取《礼记·经解》「君子慎始，差若毫厘，缪以千里」。
package model

import (
	"encoding/json"
	"time"
)

// 优先级：与前端展示的「高 / 中 / 低 / 无」一一对应。
const (
	PriorityNone   = 0
	PriorityLow    = 1
	PriorityMedium = 2
	PriorityHigh   = 3
)

// 任务状态：todo 未开始、in_progress 进行中、done 已完成。
// 「进行中」是执行层的表态——开始做了但还没收尾，与「到期日」这种时间属性分开。
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

// 任务间关联的类型：related 互相引用（对称），blocked_by 依赖阻塞（有向）。
const (
	LinkRelated  = "related"
	LinkBlocked  = "blocked_by" // 本任务被 linkedTaskId 阻塞：对方不完成，本任务不宜开工
	LinkBlocking = "blocks"     // 查询装配时的反向视图：本任务阻塞着对方
)

// 智能清单（虚拟清单）标识。这些不是数据库实体，由查询条件驱动。
const (
	SmartInbox      = "inbox"
	SmartToday      = "today"
	SmartTomorrow   = "tomorrow"   // 明天到期
	SmartWeek       = "week"       // 本周内（至本周日）
	SmartNext7      = "next7"
	SmartOverdue    = "overdue"
	SmartAll        = "all"
	SmartDone       = "done"
	SmartNoDate     = "nodate"
	SmartHigh       = "high"       // 高优先级
	SmartStarred    = "starred"    // 收藏
	SmartUpdated    = "updated"    // 最近修改
	SmartRecentDone = "recentdone" // 最近完成
)

// 重复任务的续期基准：从原到期日往后推，还是从实际完成日往后推。
// 前者保持节奏不乱，后者适合「做完了再算下一次」的松散安排。
const (
	RepeatFromDue  = "due"
	RepeatFromDone = "done"
)

// Now 返回本地时间的 RFC3339 字符串，全项目统一时间序列化格式。
func Now() string {
	return time.Now().Format(time.RFC3339)
}

// Offset 返回相对当前时间偏移若干天的 RFC3339 字符串。
func Offset(days int) string {
	return time.Now().AddDate(0, 0, days).Format(time.RFC3339)
}

// Folder 清单分组：用于把若干清单归入同一主题（如「工作」「生活」）。
// 分组可以再嵌套分组（ParentID），Children 是查询时装配出来的子树。
type Folder struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parentId"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	Collapsed bool   `json:"collapsed"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"createdAt"`
	Lists     []List   `json:"lists"`
	Children  []Folder `json:"children"`
}

// List 清单：任务的直接归属容器。
type List struct {
	ID        int64  `json:"id"`
	FolderID  *int64 `json:"folderId"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	Archived  bool   `json:"archived"`
	Starred   bool   `json:"starred"`
	CreatedAt string `json:"createdAt"`
	TaskCount int    `json:"taskCount"` // 未完成任务数
}

// Task 任务。
type Task struct {
	ID         int64   `json:"id"`
	ListID     int64   `json:"listId"`
	Title      string  `json:"title"`
	Notes      string  `json:"notes"`
	Status     string  `json:"status"`
	Priority   int     `json:"priority"`
	StartDate  *string `json:"startDate"` // YYYY-MM-DD，计划开始日
	DueDate    *string `json:"dueDate"`   // YYYY-MM-DD
	DueTime    *string `json:"dueTime"`   // HH:MM
	EndTime    *string `json:"endTime"`   // HH:MM
	URL        string  `json:"url"`       // 关联链接（会议、文档、单号）
	Reminders  []int   `json:"reminders"`
	RepeatRule *string `json:"repeatRule"`
	// 重复任务的续期基准：due（默认，从原到期日推）| done（从实际完成日推）。
	RepeatFrom string `json:"repeatFrom"`
	Important  bool   `json:"important"` // 四象限：重要
	Urgent     bool   `json:"urgent"`    // 四象限：紧急
	Pinned     bool   `json:"pinned"`    // 置顶：始终排在未完成列表最前
	Starred    bool   `json:"starred"`   // 收藏：进入「收藏」智能清单
	Archived   bool   `json:"archived"`  // 归档：从日常视野收起，侧栏「已归档」可恢复

	EstimateMinutes int `json:"estimateMinutes"` // 预计时长（分钟），0 表示未估
	Progress        int `json:"progress"`        // 手工进度百分比 0-100；有子任务时可与其完成度互为印证

	CompletedAt *string `json:"completedAt"`
	SortOrder   float64 `json:"sortOrder"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`

	// 关联数据（查询时装配）
	Subtasks    []Subtask    `json:"subtasks"`
	Attachments []Attachment `json:"attachments"`
	Tags        []Tag        `json:"tags"`
	Links       []TaskLink   `json:"links"`
	ListName    string       `json:"listName"`
	ListColor   string       `json:"listColor"`
	FolderID    *int64       `json:"folderId"`

	SubtaskDone int `json:"subtaskDone"`
	SubtaskOpen int `json:"subtaskOpen"`
}

// Attachment 任务附件。内容落在数据目录的 attachments/ 下，库里只留元数据，
// 这样数据库不会随一个几 MB 的截图急剧膨胀。
type Attachment struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"taskId"`
	Name      string `json:"name"`
	File      string `json:"file"` // 相对附件目录的存储名
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	CreatedAt string `json:"createdAt"`
}

// Webhook 出站钩子：把库内的变更推给外部系统。
// Secret 用于生成 HMAC 签名；列表一律脱敏（置空），只以 HasSecret 示意是否配过。
type Webhook struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Secret    string   `json:"secret"`
	HasSecret bool     `json:"hasSecret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// WebhookInput 是 Webhook 的写入入参，字段缺席表示不改动。
type WebhookInput struct {
	Name    Opt[string]   `json:"name"`
	URL     Opt[string]   `json:"url"`
	Secret  Opt[string]   `json:"secret"`
	Events  Opt[[]string] `json:"events"`
	Enabled Opt[bool]     `json:"enabled"`
}

// WebhookDelivery 一次投递的结果，只保留最近若干条，用于排查「为什么没收到」。
type WebhookDelivery struct {
	ID        int64  `json:"id"`
	WebhookID int64  `json:"webhookId"`
	Event     string `json:"event"`
	Code      int    `json:"code"`
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	CreatedAt string `json:"createdAt"`
}

// TaskTemplate 模板任务：把「每周例会」这类反复要做的事存成底稿，
// 需要时按它生成一条真正的任务（dueOffset 决定日期落在几天后）。
type TaskTemplate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Notes      string   `json:"notes"`
	ListID     *int64   `json:"listId"`
	Priority   int      `json:"priority"`
	DueOffset  *int     `json:"dueOffset"` // 相对生成日的天数偏移
	DueTime    *string  `json:"dueTime"`
	Reminders  []int    `json:"reminders"`
	RepeatRule *string  `json:"repeatRule"`
	Important  bool     `json:"important"`
	Urgent     bool     `json:"urgent"`
	TagIDs     []int64  `json:"tagIds"`
	Subtasks   []string `json:"subtasks"`
	SortOrder  float64  `json:"sortOrder"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
}

// TemplateInput 是模板任务的写入入参，字段缺席表示不改动。
type TemplateInput struct {
	Name       Opt[string]   `json:"name"`
	Title      Opt[string]   `json:"title"`
	Notes      Opt[string]   `json:"notes"`
	ListID     Opt[*int64]   `json:"listId"`
	Priority   Opt[int]      `json:"priority"`
	DueOffset  Opt[*int]     `json:"dueOffset"`
	DueTime    Opt[*string]  `json:"dueTime"`
	Reminders  Opt[[]int]    `json:"reminders"`
	RepeatRule Opt[*string]  `json:"repeatRule"`
	Important  Opt[bool]     `json:"important"`
	Urgent     Opt[bool]     `json:"urgent"`
	TagIDs     Opt[[]int64]  `json:"tagIds"`
	Subtasks   Opt[[]string] `json:"subtasks"`
	SortOrder  Opt[float64]  `json:"sortOrder"`
}

// CalDAVChange 是 CalDAV 增量同步用的一行变更记录（RFC 6578）。
type CalDAVChange struct {
	Seq        int64  `json:"seq"`
	Collection string `json:"collection"`
	UID        string `json:"uid"`
	TaskID     *int64 `json:"taskId"`
	Deleted    bool   `json:"deleted"`
	ChangedAt  string `json:"changedAt"`
}

// 数据变更事件名，供 Webhook 与 CalDAV 同步消费。
const (
	EventTaskCreated   = "task.created"
	EventTaskUpdated   = "task.updated"
	EventTaskCompleted = "task.completed"
	EventTaskReopened  = "task.reopened"
	EventTaskDeleted   = "task.deleted"
)

// WebhookEvents 列出可订阅的事件，供界面与校验共用。
var WebhookEvents = []string{
	EventTaskCreated, EventTaskUpdated, EventTaskCompleted, EventTaskReopened, EventTaskDeleted,
}

// Subtask 子任务。子任务可以再套子任务（ParentID），形成任务分解树；
// 日期与提醒让一条子步骤自己有「什么时候做、什么时候该被叫醒」的完整节律。
type Subtask struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"taskId"`
	ParentID  *int64    `json:"parentId"`
	Title     string    `json:"title"`
	DueDate   *string   `json:"dueDate"`
	Reminders []int     `json:"reminders"`
	Done      bool      `json:"done"`
	SortOrder int       `json:"sortOrder"`
	Children  []Subtask `json:"children"`
}

// TaskLink 任务间的关联。Related 是对称的互相引用；BlockedBy 是有向依赖：
// task_id 依赖 linked_task_id —— 对方不收尾，这条就悬着。
// Title/Status/ListName 是查询时从对方任务装配来的展示字段。
type TaskLink struct {
	ID           int64  `json:"id"`
	TaskID       int64  `json:"taskId"`
	LinkedTaskID int64  `json:"linkedTaskId"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	ListName     string `json:"listName"`
}

// Tag 标签：跨清单的多维度组织方式。
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
	TaskCount int    `json:"taskCount"`
}

// FocusSession 专注记录（番茄工作法）。
type FocusSession struct {
	ID        int64  `json:"id"`
	TaskID    *int64 `json:"taskId"`
	TaskTitle string `json:"taskTitle"`
	Minutes   int    `json:"minutes"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
}

// Review 日省：每日复盘记录，呼应「敬终」。
type Review struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"` // YYYY-MM-DD
	Mood      string `json:"mood"`
	Wins      string `json:"wins"`
	Blockers  string `json:"blockers"`
	Tomorrow  string `json:"tomorrow"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Opt 是三态可选字段：缺席（Set=false）、显式置空（Set=true 且 Value 为零值）、
// 以及正常赋值。用于 PATCH 语义，避免「未传字段」被误当成「清空字段」。
type Opt[T any] struct {
	Set   bool
	Value T
}

// UnmarshalJSON 实现三态解析。
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		var zero T
		o.Value = zero
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

// MarshalJSON 序列化内部值。
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.Value)
}

// TaskInput 是任务创建 / 更新的入参。
type TaskInput struct {
	Title      Opt[string]    `json:"title"`
	Notes      Opt[string]    `json:"notes"`
	ListID     Opt[int64]     `json:"listId"`
	Status     Opt[string]    `json:"status"`
	Priority   Opt[int]       `json:"priority"`
	StartDate  Opt[*string]   `json:"startDate"`
	DueDate    Opt[*string]   `json:"dueDate"`
	DueTime    Opt[*string]   `json:"dueTime"`
	EndTime    Opt[*string]   `json:"endTime"`
	URL        Opt[string]    `json:"url"`
	Reminders  Opt[[]int]     `json:"reminders"`
	RepeatRule Opt[*string]   `json:"repeatRule"`
	RepeatFrom Opt[string]    `json:"repeatFrom"`
	Important  Opt[bool]      `json:"important"`
	Urgent     Opt[bool]      `json:"urgent"`
	Pinned     Opt[bool]      `json:"pinned"`
	Starred    Opt[bool]      `json:"starred"`
	Archived   Opt[bool]      `json:"archived"`
	TagIDs     Opt[[]int64]   `json:"tagIds"`
	Subtasks   Opt[[]Subtask] `json:"subtasks"`
	SortOrder  Opt[float64]   `json:"sortOrder"`

	EstimateMinutes Opt[int] `json:"estimateMinutes"`
	Progress        Opt[int] `json:"progress"`
}

// SavedFilter 保存下来的筛选条件。Query 存前端 TaskFilter 的 JSON 原文，
// 服务端只负责保管与排序，不解释它的内容 —— 筛选维度会随版本变化，
// 让两端各自解析一份是自找麻烦。
type SavedFilter struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Query     string  `json:"query"`
	SortOrder float64 `json:"sortOrder"`
	CreatedAt string  `json:"createdAt"`
}

// 操作历史的事件类型。只记「值得回看」的动作，不做逐字段的流水账。
const (
	ActCreated    = "created"
	ActCompleted  = "completed"
	ActReopened   = "reopened"
	ActDeleted    = "deleted"
	ActDuplicated = "duplicated"
	ActMoved      = "moved"
	ActPurged     = "purged"
	ActUndone     = "undone"
	ActArchived   = "archived"
	ActUnarchived = "unarchived"
)

// Activity 一条操作历史。
type Activity struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	TaskID    *int64 `json:"taskId"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

// 习惯的重复节奏。
const (
	CadenceDaily  = "daily"  // 每天
	CadenceWeekly = "weekly" // 每周指定星期几
)

// Habit 习惯：需要长期重复的修身之事，与「一次性完成」的任务分开建模。
// 逻辑取自《礼记·中庸》「致中和，天地位焉」——积日成习，贵在连续。
type Habit struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Icon      string  `json:"icon"`
	Color     string  `json:"color"`
	Cadence   string  `json:"cadence"`
	Weekdays  string  `json:"weekdays"` // 逗号分隔，0=周日
	Target    int     `json:"target"`   // 单次达标所需打卡次数
	StartDate string  `json:"startDate"`
	Note      string  `json:"note"`
	Archived  bool    `json:"archived"`
	SortOrder float64 `json:"sortOrder"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// HabitLog 习惯打卡流水：一天一行。
type HabitLog struct {
	ID        int64  `json:"id"`
	HabitID   int64  `json:"habitId"`
	Day       string `json:"day"` // YYYY-MM-DD
	Count     int    `json:"count"`
	Note      string `json:"note"`
	CreatedAt string `json:"createdAt"`
}

// HabitStat 习惯在某一区间内的统计。
type HabitStat struct {
	HabitID int64   `json:"habitId"`
	Streak  int     `json:"streak"`  // 当前连续（按节奏计）
	Best    int     `json:"best"`    // 历史最长连续
	Done    int     `json:"done"`    // 区间内达标次数
	Due     int     `json:"due"`     // 区间内应达标次数
	Rate    float64 `json:"rate"`    // Done / Due
	Today   bool    `json:"today"`   // 今日是否已达标
	TodayAt int     `json:"todayAt"` // 今日已打卡次数
}

// HabitBoard 是习惯视图一次性需要的全部数据：习惯、区间打卡流水与统计。
type HabitBoard struct {
	From   string      `json:"from"`
	To     string      `json:"to"`
	Today  string      `json:"today"`
	Habits []Habit     `json:"habits"`
	Logs   []HabitLog  `json:"logs"`
	Stats  []HabitStat `json:"stats"`
}

// Stats 统计数据，供「敬终」复盘与统计视图使用。
type Stats struct {
	TotalOpen    int          `json:"totalOpen"`
	TotalDone    int          `json:"totalDone"`
	TotalAll     int          `json:"totalAll"`
	DoneToday    int          `json:"doneToday"`
	DueToday     int          `json:"dueToday"`
	DueTodayDone int          `json:"dueTodayDone"`
	Overdue      int          `json:"overdue"`
	Completion   float64      `json:"completion"`
	StreakDays   int          `json:"streakDays"`
	FocusMinutes int          `json:"focusMinutes"`
	Trend        []TrendPoint `json:"trend"`
	ByList       []CountByKey `json:"byList"`
	ByPriority   []CountByKey `json:"byPriority"`
	ByQuadrant   []CountByKey `json:"byQuadrant"`
}

// TrendPoint 单日趋势点。
type TrendPoint struct {
	Date    string `json:"date"`
	Created int    `json:"created"`
	Done    int    `json:"done"`
	Focus   int    `json:"focus"`
}

// CountByKey 通用分组计数。
type CountByKey struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}
