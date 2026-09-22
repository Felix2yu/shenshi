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

// 任务状态
const (
	StatusTodo = "todo"
	StatusDone = "done"
)

// 智能清单（虚拟清单）标识。这些不是数据库实体，由查询条件驱动。
const (
	SmartInbox   = "inbox"
	SmartToday   = "today"
	SmartNext7   = "next7"
	SmartOverdue = "overdue"
	SmartAll     = "all"
	SmartDone    = "done"
	SmartNoDate  = "nodate"
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
type Folder struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	Collapsed bool   `json:"collapsed"`
	CreatedAt string `json:"createdAt"`
	Lists     []List `json:"lists"`
}

// List 清单：任务的直接归属容器。
type List struct {
	ID        int64  `json:"id"`
	FolderID  *int64 `json:"folderId"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
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
	DueDate    *string `json:"dueDate"` // YYYY-MM-DD
	DueTime    *string `json:"dueTime"` // HH:MM
	EndTime    *string `json:"endTime"` // HH:MM
	Reminders  []int   `json:"reminders"`
	RepeatRule *string `json:"repeatRule"`
	Important  bool    `json:"important"` // 四象限：重要
	Urgent     bool    `json:"urgent"`    // 四象限：紧急

	CompletedAt *string `json:"completedAt"`
	SortOrder   float64 `json:"sortOrder"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`

	// 关联数据（查询时装配）
	Subtasks  []Subtask `json:"subtasks"`
	Tags      []Tag     `json:"tags"`
	ListName  string    `json:"listName"`
	ListColor string    `json:"listColor"`
	FolderID  *int64    `json:"folderId"`

	SubtaskDone int `json:"subtaskDone"`
	SubtaskOpen int `json:"subtaskOpen"`
}

// Subtask 子任务。
type Subtask struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"taskId"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	SortOrder int    `json:"sortOrder"`
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
	DueDate    Opt[*string]   `json:"dueDate"`
	DueTime    Opt[*string]   `json:"dueTime"`
	EndTime    Opt[*string]   `json:"endTime"`
	Reminders  Opt[[]int]     `json:"reminders"`
	RepeatRule Opt[*string]   `json:"repeatRule"`
	Important  Opt[bool]      `json:"important"`
	Urgent     Opt[bool]      `json:"urgent"`
	TagIDs     Opt[[]int64]   `json:"tagIds"`
	Subtasks   Opt[[]Subtask] `json:"subtasks"`
	SortOrder  Opt[float64]   `json:"sortOrder"`
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
