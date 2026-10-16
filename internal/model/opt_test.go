package model

import (
	"encoding/json"
	"testing"
)

// TestOptTristate 三态字段是 PATCH 语义的根基：
// 字段缺席 → Set=false（保持原值）；显式 null → Set=true + 零值（清空）；
// 有值 → Set=true + 该值。
func TestOptTristate(t *testing.T) {
	var in TaskInput
	if err := json.Unmarshal([]byte(`{"title":"写周报"}`), &in); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !in.Title.Set || in.Title.Value != "写周报" {
		t.Errorf("title: Set=%v Value=%q", in.Title.Set, in.Title.Value)
	}
	if in.Notes.Set {
		t.Error("缺席字段应保持 Set=false，调用方据此跳过更新")
	}
	if in.DueDate.Set {
		t.Error("缺席的 dueDate 不应被标成已设置")
	}

	var clr TaskInput
	if err := json.Unmarshal([]byte(`{"dueDate":null}`), &clr); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !clr.DueDate.Set {
		t.Error("显式 null 必须 Set=true，表示「清空日期」而非「未传」")
	}
	if clr.DueDate.Value != nil {
		t.Errorf("null 应给出零值，得到 %v", *clr.DueDate.Value)
	}

	var num TaskInput
	if err := json.Unmarshal([]byte(`{"priority":2}`), &num); err != nil {
		t.Fatalf("unmarshal priority: %v", err)
	}
	if !num.Priority.Set || num.Priority.Value != 2 {
		t.Errorf("priority: Set=%v Value=%d", num.Priority.Set, num.Priority.Value)
	}
}

// TestOptMarshal 序列化原样带出内部值（含 null 清空后的 *string 零值）。
func TestOptMarshal(t *testing.T) {
	o := Opt[string]{Set: true, Value: "hi"}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"hi"` {
		t.Errorf("marshal = %s，期望 \"hi\"", b)
	}

	var p Opt[*string]
	if err := json.Unmarshal([]byte(`null`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, err = json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal after null: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("清空后的字段应序列化为 null，得到 %s", b)
	}
}
