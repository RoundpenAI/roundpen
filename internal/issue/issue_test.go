package issue

import "testing"

func TestAdvance(t *testing.T) {
	cases := []struct {
		cur string
		ev  Event
		want string
	}{
		{StatusNew, EventSpecCurrent, StatusSpecReview},
		{StatusSpecReview, EventSpecCurrent, StatusSpecReview}, // 再写一版 spec 不回退
		{StatusNew, EventPlanCurrent, StatusTriaged},           // 没写 spec 也能前进
		{StatusSpecReview, EventPlanCurrent, StatusTriaged},
		{StatusNeedsDiscussion, EventPlanCurrent, StatusTriaged},
		{StatusTriaged, EventTaskOpen, StatusRunning},
		{StatusClaimed, EventTaskOpen, StatusRunning},
		{StatusClosed, EventTaskOpen, StatusRunning}, // 任务重开，事项回到执行中
		{StatusRunning, EventAllTasksDone, StatusVerified},
		{StatusTriaged, EventAllTasksDone, StatusVerified},
		{StatusNew, EventAllTasksDone, StatusVerified},
		{StatusCancelled, EventSpecCurrent, StatusCancelled}, // 取消后不自动复活
		{StatusCancelled, EventAllTasksDone, StatusCancelled},
		{StatusClosed, EventAllTasksDone, StatusClosed},
	}
	for _, c := range cases {
		if got := Advance(c.cur, c.ev); got != c.want {
			t.Fatalf("Advance(%s, %s)=%s want %s", c.cur, c.ev, got, c.want)
		}
	}
}

func TestCanTransition(t *testing.T) {
	if !CanTransition(StatusNew, StatusSpecReview) {
		t.Fatal("new → spec_review")
	}
	if !CanTransition(StatusNew, StatusTriaged) {
		t.Fatal("new → triaged fast path")
	}
	if CanTransition(StatusNew, StatusRunning) {
		t.Fatal("new → running must be rejected")
	}
	if !CanTransition(StatusFailed, StatusTriaged) {
		t.Fatal("failed → triaged requeue")
	}
	if !CanTransition(StatusClosed, StatusRunning) {
		t.Fatal("closed → running reopen")
	}
	if !CanTransition(StatusRunning, StatusRunning) {
		t.Fatal("no-op same status ok")
	}
	targets := AllowedTargets(StatusRunning)
	if len(targets) != 4 {
		t.Fatalf("running targets: %v", targets)
	}
}

func TestAllTasksDone(t *testing.T) {
	if AllTasksDone([]Task{{Status: TaskCancelled}, {Status: TaskCancelled}}) {
		t.Fatal("all cancelled is not done")
	}
	if !AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskCancelled}}) {
		t.Fatal("done+cancelled is done")
	}
	if AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskTodo}}) {
		t.Fatal("open todo is not done")
	}
}

func TestValidate(t *testing.T) {
	if err := ValidateDocStatus("weird"); err == nil {
		t.Fatal("doc status must be draft|current|superseded")
	}
	if err := ValidateIssueStatus("drafting"); err == nil {
		t.Fatal("legacy drafting must be rejected")
	}
	if err := ValidateIssueStatus(StatusNeedsReview); err != nil {
		t.Fatal(err)
	}
}
