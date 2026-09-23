// Package caldav 把「慎始」的任务以 CalDAV 的方式暴露出去，
// 让系统自带的日历与提醒事项可以直接订阅。
//
// 这里的每一处「不优雅」几乎都对应一个客户端兼容问题，尤其是 Apple 的：
//   - 任何未实现的方法都返回 403，绝不返回 501 —— Apple 日历见到 501 就报
//     「位置不支持此请求」，而且不会重试。
//   - 集合路径一律带尾斜杠，比较时用 path.Clean 归一化 —— Apple 刷新账户时
//     会自己补上斜杠，精确字符串比较会 404。
//   - Dav 头要出现在每一个响应上（包括 401 挑战），否则账户设置阶段就探测不到
//     calendar-access 能力。
//   - well-known 重定向用 302：库默认用 308，Apple 客户端跟随得更差。
package caldav

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	emcaldav "github.com/emersion/go-webdav/caldav"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// 固定路径。带尾斜杠是 CalDAV 的惯例，也是 Apple 客户端的行为。
//
// 层级不能随意压：go-webdav 靠「去掉前缀后还剩几段」判定这是主体、主目录、
// 集合还是对象（/user/ → 主体，/user/calendars/ → 主目录，
// /user/calendars/xxx/ → 集合，再深一层 → 对象）。把集合直接挂在 /user/ 下面，
// 它会被当成主目录，PROPFIND 就只回一个空的 multistatus。
const (
	PrincipalPath = "/caldav/user/"
	HomeSetPath   = "/caldav/user/calendars/"
	EventsPath    = "/caldav/user/calendars/shenshi/"
	TasksPath     = "/caldav/user/calendars/shenshi-tasks/"
)

// 集合标识，同时用作变更日志里的 collection 列。
const (
	CollectionEvents = "events"
	CollectionTasks  = "todos"
)

// AllowHeader 必须包含 PUT/DELETE：Apple 提醒事项靠 PUT 改任务状态；
// 也必须包含 PROPPATCH，否则客户端在写属性时会认为服务器不支持。
const AllowHeader = "OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, COPY, MOVE, MKCOL"

// Backend 把任务表映射成两个只读集合：日历（有日期的任务）与提醒事项（全部任务）。
// 写操作只放开「提醒事项的完成状态」一条缝——在手机上勾掉一件事是最自然的诉求，
// 而允许随意改写标题日期会让服务端失去权威。
type Backend struct {
	st *store.Store
}

// NewBackend 构造一个 CalDAV 后端。
func NewBackend(st *store.Store) *Backend { return &Backend{st: st} }

// Compile-time check：接口变了要在编译期就发现，而不是等客户端报 501。
var _ emcaldav.Backend = (*Backend)(nil)

// CurrentUserPrincipal 返回当前用户主体路径。
func (b *Backend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return PrincipalPath, nil
}

// CalendarHomeSetPath 返回日历主目录。必须比主体深一层、比集合浅一层。
func (b *Backend) CalendarHomeSetPath(ctx context.Context) (string, error) { return HomeSetPath, nil }

// CreateCalendar 不支持新建日历。返回 403 而非 501：Apple 见到 501 会直接放弃同步。
func (b *Backend) CreateCalendar(ctx context.Context, calendar *emcaldav.Calendar) error {
	return webdav.NewHTTPError(403, fmt.Errorf("caldav: 不支持新建日历"))
}

// ListCalendars 列出两个集合。
func (b *Backend) ListCalendars(ctx context.Context) ([]emcaldav.Calendar, error) {
	return []emcaldav.Calendar{
		{
			Path:                  EventsPath,
			Name:                  "慎始 · 日程",
			Description:           "有日期的任务（只读）",
			MaxResourceSize:       1 << 20,
			SupportedComponentSet: []string{ical.CompEvent},
		},
		{
			Path:                  TasksPath,
			Name:                  "慎始 · 提醒事项",
			Description:           "全部任务，可在客户端勾选完成",
			MaxResourceSize:       1 << 20,
			SupportedComponentSet: []string{ical.CompToDo},
		},
	}, nil
}

// GetCalendar 按路径取集合。用 path.Clean 归一化，容忍尾斜杠差异。
func (b *Backend) GetCalendar(ctx context.Context, p string) (*emcaldav.Calendar, error) {
	switch normalizePath(p) {
	case normalizePath(EventsPath):
		c, _ := b.calendar(EventsPath)
		return &c, nil
	case normalizePath(TasksPath):
		c, _ := b.calendar(TasksPath)
		return &c, nil
	}
	return nil, webdav.NewHTTPError(404, fmt.Errorf("caldav: 日历不存在: %s", p))
}

func (b *Backend) calendar(path string) (emcaldav.Calendar, string) {
	if path == TasksPath {
		return emcaldav.Calendar{
			Path:                  TasksPath,
			Name:                  "慎始 · 提醒事项",
			Description:           "全部任务，可在客户端勾选完成",
			MaxResourceSize:       1 << 20,
			SupportedComponentSet: []string{ical.CompToDo},
		}, CollectionTasks
	}
	return emcaldav.Calendar{
		Path:                  EventsPath,
		Name:                  "慎始 · 日程",
		Description:           "有日期的任务（只读）",
		MaxResourceSize:       1 << 20,
		SupportedComponentSet: []string{ical.CompEvent},
	}, CollectionEvents
}

// ListCalendarObjects 列出集合内的全部对象。
// 客户端发来的 time-range 过滤这里不细究：任务量级有限，全量返回更省心，
// 客户端自己会按时间裁剪显示。
func (b *Backend) ListCalendarObjects(ctx context.Context, path string, req *emcaldav.CalendarCompRequest) ([]emcaldav.CalendarObject, error) {
	_, kind, err := b.resolveCollection(path)
	if err != nil {
		return nil, err
	}
	tasks, err := b.st.ListTasks(store.TaskFilter{Status: "all", SortBy: "created"})
	if err != nil {
		return nil, unavailable(err)
	}
	out := []emcaldav.CalendarObject{}
	for i := range tasks {
		t := tasks[i]
		if kind == CollectionEvents && t.DueDate == nil {
			continue // 日历只放有日期的事，没定日期的留给提醒事项
		}
		obj, err := renderObject(kind, &t)
		if err != nil {
			continue
		}
		out = append(out, *obj)
	}
	return out, nil
}

// GetCalendarObject 按路径取单个对象。
func (b *Backend) GetCalendarObject(ctx context.Context, path string, req *emcaldav.CalendarCompRequest) (*emcaldav.CalendarObject, error) {
	kind, id, err := parseObjectPath(path)
	if err != nil {
		return nil, err
	}
	t, err := b.st.GetTask(id)
	if err != nil {
		return nil, notFound(err)
	}
	return renderObject(kind, t)
}

// QueryCalendarObjects 走与列表相同的口径：不做服务端过滤。
func (b *Backend) QueryCalendarObjects(ctx context.Context, path string, query *emcaldav.CalendarQuery) ([]emcaldav.CalendarObject, error) {
	return b.ListCalendarObjects(ctx, path, nil)
}

// PutCalendarObject 只接受提醒事项集合里的完成状态更新。
// 写完立刻从库里重新渲染权威对象返回，避免客户端以为自己的写法被采纳了。
func (b *Backend) PutCalendarObject(ctx context.Context, path string, cal *ical.Calendar, opts *emcaldav.PutCalendarObjectOptions) (*emcaldav.CalendarObject, error) {
	kind, id, err := parseObjectPath(path)
	if err != nil {
		return nil, err
	}
	if kind != CollectionTasks {
		return nil, webdav.NewHTTPError(403, fmt.Errorf("caldav: 日程集合为只读"))
	}
	if cal == nil {
		return nil, webdav.NewHTTPError(400, fmt.Errorf("caldav: 请求体不是合法的 iCalendar"))
	}

	completed, hasStatus := todoCompleted(cal)
	if !hasStatus {
		// 客户端想改标题/日期：不接受。返回 403 让客户端停止重试，而不是 501。
		return nil, webdav.NewHTTPError(403, fmt.Errorf("caldav: 只支持更新完成状态"))
	}

	cur, err := b.st.GetTask(id)
	if err != nil {
		return nil, notFound(err)
	}
	want := model.StatusDone
	if !completed {
		want = model.StatusTodo
	}
	if cur.Status != want {
		if _, err := b.st.ToggleTask(id); err != nil {
			return nil, unavailable(err)
		}
	}
	t, err := b.st.GetTask(id)
	if err != nil {
		return nil, notFound(err)
	}
	return renderObject(kind, t)
}

// DeleteCalendarObject 不允许从日历端删除任务：一次误删在手机上是很难挽回的。
func (b *Backend) DeleteCalendarObject(ctx context.Context, path string) error {
	return webdav.NewHTTPError(403, fmt.Errorf("caldav: 不支持从客户端删除任务"))
}

// ---------- 变更日志 ----------

// SyncHook 返回一个 store 事件钩子，把任务变更写进 CalDAV 变更日志，
// 供 RFC 6578 增量同步消费。注册一次即可，重复注册会产生重复日志。
func (b *Backend) SyncHook() store.EventHook {
	return func(kind string, payload any) {
		t, ok := payload.(*model.Task)
		if !ok || t == nil {
			return
		}
		deleted := kind == model.EventTaskDeleted
		// 日历集合只放有日期的任务。没有日期（含刚被清空的）或已删除，
		// 都要给客户端记一条删除，把可能残留的 VEVENT 撤掉——只在
		// DueDate != nil 时记变更，清空日期就会留下永远不消失的僵尸日程。
		// 对从未进过日历的无日期任务，这条删除是幂等无害的 404。
		_ = b.st.NoteCalDAVChange(CollectionEvents, EventUID(t.ID), t.ID, deleted || t.DueDate == nil)
		_ = b.st.NoteCalDAVChange(CollectionTasks, TodoUID(t.ID), t.ID, deleted)
	}
}

// EventUID 日历集合里的对象标识。
func EventUID(id int64) string { return fmt.Sprintf("shenshi-ev-%d", id) }

// TodoUID 提醒事项集合里的对象标识。
func TodoUID(id int64) string { return fmt.Sprintf("shenshi-td-%d", id) }

// ---------- iCalendar 渲染 ----------

// encodeCalendar 把 iCalendar 渲染成文本。ETag 与增量同步都基于这份文本，
// 因此渲染必须是确定性的（不能用 time.Now）。
func encodeCalendar(cal *ical.Calendar) (string, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderObject 渲染一个对象，并顺带算出稳定的 ETag 与内容长度。
func renderObject(kind string, t *model.Task) (*emcaldav.CalendarObject, error) {
	cal, err := renderCalendar(kind, t)
	if err != nil {
		return nil, err
	}
	text, err := encodeCalendar(cal)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	mod := parseStamp(t.UpdatedAt)
	return &emcaldav.CalendarObject{
		Path:          objectPath(kind, t.ID),
		ModTime:       mod,
		ContentLength: int64(len(text)),
		// ETag 必须稳定：内容没变就不能变，否则客户端每次都会全量重拉。
		ETag: `"` + hex.EncodeToString(sum[:16]) + `"`,
		Data: cal,
	}, nil
}

func renderCalendar(kind string, t *model.Task) (*ical.Calendar, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//shenshi//慎始 CalDAV//ZH")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText("X-WR-CALNAME", calName(kind))

	var comp *ical.Component
	switch kind {
	case CollectionEvents:
		comp = ical.NewComponent(ical.CompEvent)
	default:
		comp = ical.NewComponent(ical.CompToDo)
	}

	// DTSTAMP 用任务的更新时间而不是 time.Now()：否则每次渲染文本都变，
	// ETag 也就跟着变，客户端会以为所有条目都在变。
	stamp := parseStamp(t.UpdatedAt)
	if stamp.IsZero() {
		stamp = parseStamp(t.CreatedAt)
	}
	if stamp.IsZero() {
		stamp = time.Unix(0, 0).UTC()
	}
	comp.Props.SetText(ical.PropUID, uidFor(kind, t.ID))
	comp.Props.SetDateTime(ical.PropDateTimeStamp, stamp)
	comp.Props.SetText(ical.PropSummary, t.Title)
	if strings.TrimSpace(t.Notes) != "" {
		comp.Props.SetText(ical.PropDescription, t.Notes)
	}
	if len(t.Tags) > 0 {
		names := make([]string, 0, len(t.Tags))
		for _, g := range t.Tags {
			names = append(names, g.Name)
		}
		prop := ical.NewProp(ical.PropCategories)
		prop.SetTextList(names)
		comp.Props.Set(prop)
	}
	if t.Priority > 0 {
		comp.Props.SetText(ical.PropPriority, strconv.Itoa(priorityToRFC(t.Priority)))
	}

	start, allDay, hasDate := taskStart(t)
	if hasDate {
		if allDay {
			comp.Props.SetDate(ical.PropDateTimeStart, start)
			if kind == CollectionEvents {
				// 全天事件的 DTEND 是「次日」，含头不含尾。
				comp.Props.SetDate(ical.PropDateTimeEnd, start.AddDate(0, 0, 1))
			} else {
				comp.Props.SetDate(ical.PropDue, start)
			}
		} else {
			comp.Props.SetDateTime(ical.PropDateTimeStart, start)
			if end, ok := taskEnd(t, start); ok {
				comp.Props.SetDateTime(ical.PropDateTimeEnd, end)
				if kind == CollectionTasks {
					comp.Props.SetDateTime(ical.PropDue, end)
				}
			} else if kind == CollectionTasks {
				comp.Props.SetDateTime(ical.PropDue, start)
			}
		}
	}

	if kind == CollectionTasks {
		if t.Status == model.StatusDone {
			comp.Props.SetText(ical.PropStatus, "COMPLETED")
			comp.Props.SetText(ical.PropPercentComplete, "100")
			if t.CompletedAt != nil {
				comp.Props.SetDateTime(ical.PropCompleted, parseStamp(*t.CompletedAt))
			}
		} else {
			comp.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
			if len(t.Subtasks) > 0 {
				pct := t.SubtaskDone * 100 / len(t.Subtasks)
				comp.Props.SetText(ical.PropPercentComplete, strconv.Itoa(pct))
			}
		}
	}

	// 提醒：只带第一条，够用且不至于让客户端弹出一串闹钟。
	if len(t.Reminders) > 0 && hasDate {
		alarm := ical.NewComponent(ical.CompAlarm)
		alarm.Props.SetText("ACTION", "DISPLAY")
		alarm.Props.SetText(ical.PropDescription, t.Title)
		trigger := ical.NewProp("TRIGGER")
		trigger.Params.Set("VALUE", "DURATION")
		trigger.SetText("-" + isodur(t.Reminders[0]))
		alarm.Props.Set(trigger)
		comp.Children = append(comp.Children, alarm)
	}

	cal.Children = append(cal.Children, comp)
	return cal, nil
}

func calName(kind string) string {
	if kind == CollectionTasks {
		return "慎始 · 提醒事项"
	}
	return "慎始 · 日程"
}

// priorityToRFC 把 0-3 的优先级映射到 RFC 5545 的 1-9（1 最高）。
func priorityToRFC(p int) int {
	switch p {
	case model.PriorityHigh:
		return 1
	case model.PriorityMedium:
		return 5
	case model.PriorityLow:
		return 7
	default:
		return 0
	}
}

// isodur 把「提前 N 分钟」写成 ISO 8601 时长。
func isodur(minutes int) string {
	if minutes <= 0 {
		return "PT0S"
	}
	if minutes%1440 == 0 {
		return fmt.Sprintf("P%dD", minutes/1440)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("PT%dH", minutes/60)
	}
	return fmt.Sprintf("PT%dM", minutes)
}

// taskStart 解析任务的开始时刻。hasDate 为 false 表示没有日期。
func taskStart(t *model.Task) (start time.Time, allDay bool, ok bool) {
	if t.DueDate == nil || strings.TrimSpace(*t.DueDate) == "" {
		return time.Time{}, false, false
	}
	d, err := time.ParseInLocation("2006-01-02", *t.DueDate, time.Local)
	if err != nil {
		return time.Time{}, false, false
	}
	if t.DueTime != nil && strings.TrimSpace(*t.DueTime) != "" {
		h, m, err := parseHHMM(*t.DueTime)
		if err == nil {
			return d.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute), false, true
		}
	}
	return d, true, true
}

func taskEnd(t *model.Task, start time.Time) (time.Time, bool) {
	if t.EndTime == nil || strings.TrimSpace(*t.EndTime) == "" {
		return time.Time{}, false
	}
	h, m, err := parseHHMM(*t.EndTime)
	if err != nil {
		return time.Time{}, false
	}
	// EndTime 是当天钟点（与前端 toMinutes(endTime) 同口径），不是距开始的时长。
	end := time.Date(start.Year(), start.Month(), start.Day(), h, m, 0, 0, start.Location())
	// 结束早于开始多半是跨夜写错了，退化为一小时。
	if !end.After(start) {
		return start.Add(time.Hour), true
	}
	return end, true
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式不合法: %s", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("时间超出范围: %s", s)
	}
	return h, m, nil
}

// parseStamp 解析库里的时间串。失败时返回零值，由调用方兜底。
func parseStamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

// todoCompleted 读出客户端 PUT 上来的 VTODO 完成状态。
func todoCompleted(cal *ical.Calendar) (completed bool, hasStatus bool) {
	for _, comp := range cal.Children {
		if comp.Name != ical.CompToDo {
			continue
		}
		if p := comp.Props.Get(ical.PropStatus); p != nil {
			if v, err := p.Text(); err == nil {
				return strings.EqualFold(strings.TrimSpace(v), "COMPLETED"), true
			}
			return false, true
		}
		// 没有 STATUS 但有 COMPLETED 属性，也算勾掉了。
		if comp.Props.Get(ical.PropCompleted) != nil {
			return true, true
		}
	}
	return false, false
}

// ---------- 路径 ----------

func normalizePath(p string) string {
	c := strings.TrimSuffix(p, "/")
	if c == "" {
		return "/"
	}
	return c
}

// resolveCollection 判定路径属于哪个集合。
func (b *Backend) resolveCollection(path string) (emcaldav.Calendar, string, error) {
	switch normalizePath(path) {
	case normalizePath(EventsPath):
		c, kind := b.calendar(EventsPath)
		return c, kind, nil
	case normalizePath(TasksPath):
		c, kind := b.calendar(TasksPath)
		return c, kind, nil
	}
	return emcaldav.Calendar{}, "", webdav.NewHTTPError(404, fmt.Errorf("caldav: 日历不存在: %s", path))
}

func objectPath(kind string, id int64) string {
	if kind == CollectionTasks {
		return TasksPath + TodoUID(id) + ".ics"
	}
	return EventsPath + EventUID(id) + ".ics"
}

// parseObjectPath 从对象路径里解出集合与任务 id。
func parseObjectPath(path string) (kind string, id int64, err error) {
	dir, file := splitPath(path)
	uid := strings.TrimSuffix(file, ".ics")
	switch {
	case strings.HasPrefix(uid, "shenshi-td-"):
		kind = CollectionTasks
		uid = strings.TrimPrefix(uid, "shenshi-td-")
	case strings.HasPrefix(uid, "shenshi-ev-"):
		kind = CollectionEvents
		uid = strings.TrimPrefix(uid, "shenshi-ev-")
	default:
		return "", 0, webdav.NewHTTPError(404, fmt.Errorf("caldav: 对象不存在: %s", path))
	}
	// 集合与 uid 前缀不一致也是找不到（比如把 todo 的 uid 放到日历集合里）。
	want := TasksPath
	if kind == CollectionEvents {
		want = EventsPath
	}
	if normalizePath(dir) != normalizePath(want) {
		return "", 0, webdav.NewHTTPError(404, fmt.Errorf("caldav: 对象不存在: %s", path))
	}
	n, perr := strconv.ParseInt(uid, 10, 64)
	if perr != nil || n <= 0 {
		return "", 0, webdav.NewHTTPError(404, fmt.Errorf("caldav: 对象不存在: %s", path))
	}
	return kind, n, nil
}

func splitPath(p string) (dir, file string) {
	p = strings.TrimSuffix(p, "/")
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "/", p
	}
	return p[:i+1], p[i+1:]
}

func uidFor(kind string, id int64) string {
	if kind == CollectionTasks {
		return TodoUID(id)
	}
	return EventUID(id)
}

// ---------- 错误包装 ----------

// unavailable 把数据库错误包装成 503：客户端会重试，而 500 会被当成永久失败。
func unavailable(err error) error {
	return webdav.NewHTTPError(503, fmt.Errorf("caldav: 数据暂时不可用: %w", err))
}

func notFound(err error) error {
	return webdav.NewHTTPError(404, fmt.Errorf("caldav: 对象不存在: %w", err))
}
