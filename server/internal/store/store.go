// Package store 负责「慎始」的持久化：SQLite 连接、表结构、种子数据与全部查询。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite 驱动，无需 cgo
)

// Options 描述数据目录的布局。留空的目录按数据文件所在目录推导。
type Options struct {
	AttachmentDir string // 附件存放目录
	BackupDir     string // 自动备份存放目录
}

// Store 封装数据库句柄。
type Store struct {
	db            *sql.DB
	attachmentDir string
	backupDir     string

	hookMu sync.RWMutex
	hooks  []EventHook
}

// EventHook 是数据变更的观察者。kind 形如 task.created，payload 依 kind 而定
// （目前任务事件统一传 *model.Task）。钩子不得阻塞：实现方自行异步化。
type EventHook func(kind string, payload any)

// Open 打开（必要时创建）数据库并完成迁移与首次种子数据写入。
func Open(path string) (*Store, error) {
	return OpenWith(path, Options{})
}

// OpenWith 与 Open 相同，但允许指定附件与备份目录。
func OpenWith(path string, opt Options) (*Store, error) {
	if path == "" {
		path = filepath.Join("data", "shenshi.db")
	}
	base := filepath.Dir(path)
	if base == "" || base == "." {
		base = "data"
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 写入串行化；保留少量连接以支撑并发读，写冲突由 busy_timeout 兜底。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	s := &Store{
		db:            db,
		attachmentDir: orDefault(opt.AttachmentDir, filepath.Join(base, "attachments")),
		backupDir:     orDefault(opt.BackupDir, filepath.Join(base, "backups")),
	}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.seed(); err != nil {
		return nil, err
	}
	// 目录不存在时立刻建好，避免第一次上传/备份才失败。
	if err := os.MkdirAll(s.attachmentDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建附件目录失败: %w", err)
	}
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	return s, nil
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// AttachmentDir 返回附件存放目录。
func (s *Store) AttachmentDir() string { return s.attachmentDir }

// BackupDir 返回自动备份存放目录。
func (s *Store) BackupDir() string { return s.backupDir }

// OnEvent 注册一个变更观察者。钩子内不应执行耗时操作。
func (s *Store) OnEvent(fn EventHook) {
	if fn == nil {
		return
	}
	s.hookMu.Lock()
	s.hooks = append(s.hooks, fn)
	s.hookMu.Unlock()
}

// emit 向所有观察者广播一次变更。单个钩子 panic 不影响其它钩子与主流程。
func (s *Store) emit(kind string, payload any) {
	s.hookMu.RLock()
	hooks := make([]EventHook, len(s.hooks))
	copy(hooks, s.hooks)
	s.hookMu.RUnlock()
	for _, fn := range hooks {
		func() {
			defer func() { _ = recover() }()
			fn(kind, payload)
		}()
	}
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS folders (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,
  color      TEXT    NOT NULL DEFAULT '#8a7c66',
  icon       TEXT    NOT NULL DEFAULT 'folder',
  sort_order INTEGER NOT NULL DEFAULT 0,
  collapsed  INTEGER NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS lists (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  folder_id  INTEGER REFERENCES folders(id) ON DELETE SET NULL,
  name       TEXT    NOT NULL,
  color      TEXT    NOT NULL DEFAULT '#b4553d',
  icon       TEXT    NOT NULL DEFAULT 'list',
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_inbox   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  title       TEXT    NOT NULL,
  notes       TEXT    NOT NULL DEFAULT '',
  status      TEXT    NOT NULL DEFAULT 'todo',
  priority    INTEGER NOT NULL DEFAULT 0,
  due_date    TEXT,
  due_time    TEXT,
  end_time    TEXT,
  reminders   TEXT    NOT NULL DEFAULT '[]',
  repeat_rule TEXT,
  important   INTEGER NOT NULL DEFAULT 0,
  urgent      INTEGER NOT NULL DEFAULT 0,
  completed_at TEXT,
  sort_order  REAL    NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_list   ON tasks(list_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_due    ON tasks(due_date);

CREATE TABLE IF NOT EXISTS subtasks (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  title      TEXT    NOT NULL,
  done       INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_subtasks_task ON subtasks(task_id);

CREATE TABLE IF NOT EXISTS tags (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL UNIQUE,
  color      TEXT    NOT NULL DEFAULT '#6b7f6e',
  created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS task_tags (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  tag_id  INTEGER NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
  PRIMARY KEY (task_id, tag_id)
);

CREATE TABLE IF NOT EXISTS focus_sessions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  minutes    INTEGER NOT NULL DEFAULT 0,
  started_at TEXT    NOT NULL,
  ended_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS reviews (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  date       TEXT    NOT NULL UNIQUE,
  mood       TEXT    NOT NULL DEFAULT '',
  wins       TEXT    NOT NULL DEFAULT '',
  blockers   TEXT    NOT NULL DEFAULT '',
  tomorrow   TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- 习惯：需要「日复一日」坚持的事，与一次性任务分开建模。
CREATE TABLE IF NOT EXISTS habits (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,
  icon       TEXT    NOT NULL DEFAULT 'habit',
  color      TEXT    NOT NULL DEFAULT '#6b7f6e',
  cadence    TEXT    NOT NULL DEFAULT 'daily',   -- daily | weekly
  weekdays   TEXT    NOT NULL DEFAULT '',        -- weekly 时生效，如 '1,3,5'（0=周日）
  target     INTEGER NOT NULL DEFAULT 1,         -- 单次达标所需打卡次数
  start_date TEXT    NOT NULL,                   -- 起始日，之前的日期不参与统计与缺卡
  note       TEXT    NOT NULL DEFAULT '',
  archived   INTEGER NOT NULL DEFAULT 0,
  sort_order REAL    NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_habits_archived ON habits(archived);

-- 打卡流水：一天一行，(habit_id, day) 唯一，count 为当天次数。
CREATE TABLE IF NOT EXISTS habit_logs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  habit_id   INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
  day        TEXT    NOT NULL,
  count      INTEGER NOT NULL DEFAULT 1,
  note       TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL,
  UNIQUE(habit_id, day)
);
CREATE INDEX IF NOT EXISTS idx_habit_logs_habit ON habit_logs(habit_id, day);

-- 提醒投递台账：保证同一任务的同一触发时刻只提醒一次。
CREATE TABLE IF NOT EXISTS reminder_log (
  task_id  INTEGER NOT NULL,
  fire_at  TEXT    NOT NULL,
  fired_at TEXT    NOT NULL,
  PRIMARY KEY (task_id, fire_at)
);

-- 附件：元数据在库里，内容落在数据目录下的 attachments/。
-- 只存相对文件名，换机器时整个数据目录拷走即可。
CREATE TABLE IF NOT EXISTS attachments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  file       TEXT    NOT NULL,
  size       INTEGER NOT NULL DEFAULT 0,
  mime       TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_task ON attachments(task_id);

-- 出站 Webhook：把库里的变更推给外部系统，让「慎始」不必长成一座孤岛。
CREATE TABLE IF NOT EXISTS webhooks (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL DEFAULT '',
  url        TEXT    NOT NULL,
  secret     TEXT    NOT NULL DEFAULT '',
  events     TEXT    NOT NULL DEFAULT '[]',
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL
);

-- 投递台账：只留最近若干条，用于排查「为什么没收到」。
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  webhook_id INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event      TEXT    NOT NULL,
  code       INTEGER NOT NULL DEFAULT 0,
  ok         INTEGER NOT NULL DEFAULT 0,
  error      TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_deliveries_hook ON webhook_deliveries(webhook_id, id DESC);

-- 模板任务：把「每周例会」这类反复要做的事存成底稿，一键铺开成真正的任务。
CREATE TABLE IF NOT EXISTS task_templates (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  title       TEXT    NOT NULL,
  notes       TEXT    NOT NULL DEFAULT '',
  list_id     INTEGER REFERENCES lists(id) ON DELETE SET NULL,
  priority    INTEGER NOT NULL DEFAULT 0,
  due_offset  INTEGER,                            -- 相对创建日的天数偏移，空表示不带日期
  due_time    TEXT,
  reminders   TEXT    NOT NULL DEFAULT '[]',
  repeat_rule TEXT,
  important   INTEGER NOT NULL DEFAULT 0,
  urgent      INTEGER NOT NULL DEFAULT 0,
  tag_ids     TEXT    NOT NULL DEFAULT '[]',
  subtasks    TEXT    NOT NULL DEFAULT '[]',      -- JSON 字符串数组
  sort_order  REAL    NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);

-- CalDAV 变更日志：为 RFC 6578 增量同步提供单调递增的序号。
CREATE TABLE IF NOT EXISTS caldav_changes (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  collection TEXT    NOT NULL,                    -- events | todos
  uid        TEXT    NOT NULL,
  task_id    INTEGER,
  deleted    INTEGER NOT NULL DEFAULT 0,
  changed_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_caldav_collection ON caldav_changes(collection, seq);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("初始化表结构失败: %w", err)
	}
	return nil
}

// seedTask / seedList / seedFolder 描述初始数据的形状。
type seedTask struct {
	title     string
	notes     string
	priority  int
	due       string
	dueTime   string
	important bool
	urgent    bool
	tags      []string
	subs      []string
}

type seedList struct {
	name  string
	color string
	icon  string
	tasks []seedTask
}

type seedFolder struct {
	name  string
	color string
	icon  string
	lists []seedList
}

// seed 仅在数据库为空时写入一套带有「慎始」理念的初始数据，
// 让用户第一次打开就能看到一个有结构、可上手的起点。
func (s *Store) seed() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM lists`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	in3 := now.AddDate(0, 0, 3).Format("2006-01-02")

	folders := []seedFolder{
		{name: "工作", color: "#b4553d", icon: "briefcase", lists: []seedList{
			{name: "项目推进", color: "#b4553d", icon: "target", tasks: []seedTask{
				{title: "梳理「慎始」v1 的需求边界", notes: "把「慎始而敬终」落到可交付的功能清单上。\n- 任务创建与编辑\n- 清单分组\n- 智能提醒", priority: 3, due: today, dueTime: "10:00", important: true, urgent: true, tags: []string{"规划"}, subs: []string{"对齐参考产品的功能结构", "确定首版范围"}},
				{title: "评审交互稿并给出意见", priority: 2, due: tomorrow, dueTime: "14:30", important: true, tags: []string{"协作"}},
				{title: "输出季度复盘材料", priority: 1, due: in3, important: false, tags: []string{"汇报"}},
			}},
			{name: "会议与沟通", color: "#8a6d3b", icon: "users", tasks: []seedTask{
				{title: "周会前同步本周计划", priority: 2, due: today, dueTime: "09:20", urgent: true, tags: []string{"协作"}},
			}},
		}},
		{name: "生活", color: "#6b7f6e", icon: "home", lists: []seedList{
			{name: "个人事务", color: "#6b7f6e", icon: "user", tasks: []seedTask{
				{title: "预约体检", priority: 1, due: in3, tags: []string{"健康"}},
				{title: "读《礼记》选注 · 学记篇", notes: "「善问者如攻坚木，先其易者，后其节目。」", priority: 0, tags: []string{"阅读"}},
			}},
		}},
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	ts := modelNow()

	// 收集箱：所有未归类想法的第一落点，也是「慎始」的起点。
	if _, err := tx.Exec(`INSERT INTO lists(folder_id, name, color, icon, sort_order, is_inbox, created_at) VALUES(NULL,?,?,?,0,1,?)`,
		"收集箱", "#8a8f98", "inbox", ts); err != nil {
		return err
	}

	tagIDs := map[string]int64{}
	for _, f := range folders {
		res, err := tx.Exec(`INSERT INTO folders(name, color, icon, sort_order, collapsed, created_at) VALUES(?,?,?,?,0,?)`,
			f.name, f.color, f.icon, len(folders), ts)
		if err != nil {
			return err
		}
		fid, _ := res.LastInsertId()
		for _, l := range f.lists {
			res, err := tx.Exec(`INSERT INTO lists(folder_id, name, color, icon, sort_order, created_at) VALUES(?,?,?,?,?,?)`,
				fid, l.name, l.color, l.icon, 0, ts)
			if err != nil {
				return err
			}
			lid, _ := res.LastInsertId()
			for i, t := range l.tasks {
				var due interface{}
				if t.due != "" {
					due = t.due
				}
				var dueTime interface{}
				if t.dueTime != "" {
					dueTime = t.dueTime
				}
				res, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, due_date, due_time, reminders, important, urgent, sort_order, created_at, updated_at)
					VALUES(?,?,?,'todo',?,?,?,'[]',?,?,?,?,?)`,
					lid, t.title, t.notes, t.priority, due, dueTime, boolInt(t.important), boolInt(t.urgent), float64(i)*1024, ts, ts)
				if err != nil {
					return err
				}
				tid, _ := res.LastInsertId()
				for j, sub := range t.subs {
					if _, err := tx.Exec(`INSERT INTO subtasks(task_id, title, done, sort_order) VALUES(?,?,0,?)`, tid, sub, j); err != nil {
						return err
					}
				}
				for _, name := range t.tags {
					id, ok := tagIDs[name]
					if !ok {
						colors := []string{"#b4553d", "#6b7f6e", "#8a6d3b", "#5c6b8a", "#7a5c7a"}
						res, err := tx.Exec(`INSERT OR IGNORE INTO tags(name, color, created_at) VALUES(?,?,?)`,
							name, colors[len(tagIDs)%len(colors)], ts)
						if err != nil {
							return err
						}
						_ = res
						if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, name).Scan(&id); err != nil {
							return err
						}
						tagIDs[name] = id
					}
					if _, err := tx.Exec(`INSERT OR IGNORE INTO task_tags(task_id, tag_id) VALUES(?,?)`, tid, id); err != nil {
						return err
					}
				}
			}
		}
	}
	// 收集箱中的「待澄清」事项：先记下来，再决定归属——这是慎始的第一步。
	var inboxID int64
	if err := tx.QueryRow(`SELECT id FROM lists WHERE is_inbox=1`).Scan(&inboxID); err != nil {
		return err
	}
	inboxTasks := []seedTask{
		{title: "把「年度目标」拆成可执行的第一步", notes: "《礼记·中庸》：凡事豫则立，不豫则废。", priority: 2, important: true},
		{title: "整理本周收到的灵感与待办", priority: 0},
	}
	for i, t := range inboxTasks {
		if _, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, reminders, important, urgent, sort_order, created_at, updated_at)
			VALUES(?,?,?,'todo',?,'[0]',?,0,?,?,?)`,
			inboxID, t.title, t.notes, t.priority, boolInt(t.important), float64(i)*1024, ts, ts); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func modelNow() string { return time.Now().Format(time.RFC3339) }
