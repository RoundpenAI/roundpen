// Package issue models the user's work tracker: issues, their versioned spec/plan
// documents, and the tasks derived from those plans.
package issue

import (
	"fmt"
	"strings"
	"time"
)

const (
	StatusDrafting   = "drafting"
	StatusSpecced    = "specced"
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

const (
	TaskTodo       = "todo"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskBlocked    = "blocked"
	TaskCancelled  = "cancelled"
)

const (
	DocSpec = "spec"
	DocPlan = "plan"
)

const (
	DocDraft      = "draft"
	DocCurrent    = "current"
	DocSuperseded = "superseded"
)

const (
	OriginChat    = "chat"
	OriginConsole = "console"
)

// Event drives automatic issue status advance from document and task writes.
type Event string

const (
	EventSpecCurrent  Event = "spec_current"   // spec 写入 current
	EventPlanCurrent  Event = "plan_current"   // plan 写入 current
	EventTaskOpen     Event = "task_open"      // 任务进入 in_progress，或被重新打开
	EventAllTasksDone Event = "all_tasks_done" // 全部任务 ∈ {done,cancelled} 且 ≥1 done
)

type Issue struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	UserID      string     `json:"userId"`
	AssistantID string     `json:"assistantId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Status      string     `json:"status"`
	Origin      string     `json:"origin"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
}

type Doc struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	IssueID     string    `json:"issueId"`
	TaskID      string    `json:"taskId,omitempty"`
	Kind        string    `json:"kind"`
	Version     int       `json:"version"`
	Status      string    `json:"status"`
	Title       string    `json:"title"`
	ContentMD   string    `json:"contentMd,omitempty"` // 列表查询不返回正文，见 ListDocs
	AuthorType  string    `json:"authorType"`
	AssistantID string    `json:"assistantId,omitempty"`
	SessionID   string    `json:"sessionId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Task struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	IssueID     string     `json:"issueId"`
	PlanDocID   string     `json:"planDocId,omitempty"`
	Position    int        `json:"position"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"`
	Status      string     `json:"status"`
	SessionID   string     `json:"sessionId,omitempty"`
	AssistantID string     `json:"assistantId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DoneAt      *time.Time `json:"doneAt,omitempty"`
}

// Advance returns the issue status after ev. Forward-only: a rule that does not
// apply leaves the status unchanged, and a cancelled issue never auto-revives.
func Advance(cur string, ev Event) string {
	if cur == StatusCancelled {
		return cur
	}
	switch ev {
	case EventSpecCurrent:
		if cur == StatusDrafting {
			return StatusSpecced
		}
	case EventPlanCurrent:
		if cur == StatusDrafting || cur == StatusSpecced {
			return StatusPlanned
		}
	case EventTaskOpen:
		if cur != StatusInProgress {
			return StatusInProgress
		}
	case EventAllTasksDone:
		// Closing does not require having passed through in_progress: ticking
		// the last task is the same act whether or not the work was announced
		// as started first.
		return StatusDone
	}
	return cur
}

// AllTasksDone reports whether the task list satisfies the completion rule:
// at least one done, and nothing left in todo/in_progress/blocked. An issue whose
// tasks were all cancelled stays in_progress for the user to decide.
func AllTasksDone(tasks []Task) bool {
	if len(tasks) == 0 {
		return false
	}
	done := 0
	for _, t := range tasks {
		switch t.Status {
		case TaskDone:
			done++
		case TaskCancelled:
		default:
			return false
		}
	}
	return done > 0
}

func ValidateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

func ValidateKind(kind string) error {
	if kind != DocSpec && kind != DocPlan {
		return fmt.Errorf("kind must be spec or plan")
	}
	return nil
}

func ValidateDocStatus(status string) error {
	switch status {
	case DocDraft, DocCurrent, DocSuperseded:
		return nil
	}
	return fmt.Errorf("status must be draft, current or superseded")
}

func ValidateTaskStatus(status string) error {
	switch status {
	case TaskTodo, TaskInProgress, TaskDone, TaskBlocked, TaskCancelled:
		return nil
	}
	return fmt.Errorf("status must be todo, in_progress, done, blocked or cancelled")
}

func ValidateIssueStatus(status string) error {
	switch status {
	case StatusDrafting, StatusSpecced, StatusPlanned, StatusInProgress, StatusDone, StatusCancelled:
		return nil
	}
	return fmt.Errorf("invalid issue status %q", status)
}
