package issue

import "testing"

func TestAdvance_ForwardOnly(t *testing.T) {
	cases := []struct {
		cur  string
		ev   Event
		want string
	}{
		{StatusDrafting, EventSpecCurrent, StatusSpecced},
		{StatusSpecced, EventSpecCurrent, StatusSpecced},  // 再写一版 spec 不回退
		{StatusDrafting, EventPlanCurrent, StatusPlanned}, // 没写 spec 也能前进
		{StatusSpecced, EventPlanCurrent, StatusPlanned},
		{StatusPlanned, EventTaskOpen, StatusInProgress},
		{StatusDone, EventTaskOpen, StatusInProgress}, // 任务重开，议题回到进行中
		{StatusInProgress, EventAllTasksDone, StatusDone},
		{StatusPlanned, EventAllTasksDone, StatusDone}, // 勾完最后一个任务即完成
		{StatusSpecced, EventAllTasksDone, StatusDone}, // 不必先「开工」经过 in_progress
		{StatusDrafting, EventAllTasksDone, StatusDone},
		{StatusCancelled, EventSpecCurrent, StatusCancelled}, // 取消后不自动复活
		{StatusCancelled, EventAllTasksDone, StatusCancelled},
	}
	for _, tc := range cases {
		if got := Advance(tc.cur, tc.ev); got != tc.want {
			t.Errorf("Advance(%s, %s) = %s, want %s", tc.cur, tc.ev, got, tc.want)
		}
	}
}

func TestAllTasksDone(t *testing.T) {
	if AllTasksDone(nil) {
		t.Fatal("no tasks must not count as done")
	}
	if AllTasksDone([]Task{{Status: TaskCancelled}, {Status: TaskCancelled}}) {
		t.Fatal("all-cancelled must not count as done — issue stays in_progress for the user")
	}
	if !AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskCancelled}}) {
		t.Fatal("done + cancelled counts as done")
	}
	if AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskTodo}}) {
		t.Fatal("a todo task blocks completion")
	}
}

func TestValidateInputs(t *testing.T) {
	if err := ValidateTitle("  "); err == nil {
		t.Fatal("blank title must fail")
	}
	if err := ValidateKind("notes"); err == nil {
		t.Fatal("kind must be spec|plan")
	}
	if err := ValidateDocStatus("weird"); err == nil {
		t.Fatal("doc status must be draft|current|superseded")
	}
}
