package store

import (
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// TestRollOverdue 逾期顺延：只动「未完成 + 未归档 + 到期日早于目标日」的那些。
func TestRollOverdue(t *testing.T) {
	s := newTestStore(t)
	clearTasks(t, s)

	today := time.Now().Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	future := time.Now().AddDate(0, 0, 5).Format("2006-01-02")

	// 该顺延：逾期未完成。
	overdue := mustCreateTask(t, s, "逾期待办", func(in *model.TaskInput) {
		in.DueDate = optOf(&past)
		in.Status = optOf(model.StatusTodo)
	})
	// 不该动：已完成。（CreateTask 固定建为 todo，完成态得走 UpdateTask。）
	doneTask := mustCreateTask(t, s, "逾期但已完成", func(in *model.TaskInput) {
		in.DueDate = optOf(&past)
	})
	if _, err := s.UpdateTask(doneTask.ID, model.TaskInput{Status: optOf(model.StatusDone)}); err != nil {
		t.Fatalf("标记完成: %v", err)
	}
	// 不该动：未来到期。
	mustCreateTask(t, s, "未来到期", func(in *model.TaskInput) {
		in.DueDate = optOf(&future)
	})
	// 不该动：已归档。
	arch := mustCreateTask(t, s, "逾期但已归档", func(in *model.TaskInput) {
		in.DueDate = optOf(&past)
	})
	if _, err := s.UpdateTask(arch.ID, model.TaskInput{Archived: optOf(true)}); err != nil {
		t.Fatalf("标记归档: %v", err)
	}
	// 不该动：没有到期日。
	mustCreateTask(t, s, "无日期", nil)

	n, err := s.RollOverdue("")
	if err != nil {
		t.Fatalf("RollOverdue: %v", err)
	}
	if n != 1 {
		t.Errorf("顺延条数 = %d，期望 1", n)
	}

	got, err := s.GetTask(overdue.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.DueDate == nil || *got.DueDate != today {
		t.Errorf("逾期任务到期日 = %v，期望 %s", got.DueDate, today)
	}
	// urgent 要按新日期重算，否则拖到今天之后还挂着紧急标记。
	if !got.Urgent {
		t.Error("顺延到今天后应重新判为紧急")
	}

	// 无日期的任务不该被凭空安上日期。
	var nodue int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE title = '无日期' AND due_date IS NOT NULL`).Scan(&nodue); err != nil {
		t.Fatalf("count: %v", err)
	}
	if nodue != 0 {
		t.Error("无日期任务被安上了日期")
	}

	// 归档的与已完成的都应保持原到期日。
	for _, title := range []string{"逾期但已完成", "逾期但已归档"} {
		var d string
		if err := s.db.QueryRow(`SELECT due_date FROM tasks WHERE title = ?`, title).Scan(&d); err != nil {
			t.Fatalf("查 %s: %v", title, err)
		}
		if d != past {
			t.Errorf("%s 的到期日 = %s，期望保持 %s", title, d, past)
		}
	}
}

// TestCountOverdue 计数口径必须与顺延口径一致，否则按钮上写的数字会骗人。
func TestCountOverdue(t *testing.T) {
	s := newTestStore(t)
	clearTasks(t, s)

	today := time.Now().Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	mustCreateTask(t, s, "逾期待办", func(in *model.TaskInput) { in.DueDate = optOf(&past) })
	mustCreateTask(t, s, "逾期待办2", func(in *model.TaskInput) { in.DueDate = optOf(&past) })
	mustCreateTask(t, s, "今天到期", func(in *model.TaskInput) { in.DueDate = optOf(&today) })
	done2 := mustCreateTask(t, s, "逾期已完成", func(in *model.TaskInput) {
		in.DueDate = optOf(&past)
	})
	if _, err := s.UpdateTask(done2.ID, model.TaskInput{Status: optOf(model.StatusDone)}); err != nil {
		t.Fatalf("标记完成: %v", err)
	}

	n, err := s.CountOverdue("")
	if err != nil {
		t.Fatalf("CountOverdue: %v", err)
	}
	if n != 2 {
		t.Errorf("逾期计数 = %d，期望 2", n)
	}

	// 顺延之后计数应归零：计数和顺延口径若不一致，按钮上的数字就是错的。
	if _, err := s.RollOverdue(""); err != nil {
		t.Fatalf("RollOverdue: %v", err)
	}
	n, _ = s.CountOverdue("")
	if n != 0 {
		t.Errorf("顺延后仍报 %d 件逾期，口径不一致", n)
	}
}