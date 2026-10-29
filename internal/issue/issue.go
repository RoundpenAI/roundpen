// Package issue models the user's work tracker: issues, their versioned spec/plan
// documents, and the tasks derived from those plans.
package issue

import (
	"fmt"
	"strings"
	"time"
)

// Kanban statuses (project board). See docs in the Project store:
// issues-kanban-workflow.md
const (
	StatusNew             = "new"
	StatusSpecReview      = "spec_review"
	StatusNeedsDiscussion = "needs_discussion"
	StatusTriaged         = "triaged"
	StatusClaimed         = "claimed"
	StatusRunning         = "running"
	StatusVerified        = "verified"
	StatusNeedsReview     = "needs_review"
	StatusFailed          = "failed"
	StatusClosed          = "closed"
	StatusCancelled       = "cancelled"
)

// BoardColumnOrder is the left-to-right Kanban order. Terminal columns are last.
var BoardColumnOrder = []string{
	StatusNew,
	StatusSpecReview,
	StatusNeedsDiscussion,
	StatusTriaged,
	StatusClaimed,
	StatusRunning,
	StatusVerified,
	StatusNeedsReview,
	StatusFailed,
	StatusClosed,
	StatusCancelled,
}

// TerminalStatuses are folded by default on the board.
var TerminalStatuses = []string{StatusClosed, StatusCancelled}

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

// allowedTransitions is the authoritative edge set for human + Agent transitions.
var allowedTransitions = map[string][]string{
	StatusNew:             {StatusSpecReview, StatusNeedsDiscussion, StatusTriaged},
	StatusSpecReview:      {StatusNeedsDiscussion, StatusTriaged},
	StatusNeedsDiscussion: {StatusSpecReview, StatusTriaged},
	StatusTriaged:         {StatusClaimed},
	StatusClaimed:         {StatusRunning},
	StatusRunning:         {StatusVerified, StatusNeedsReview, StatusFailed, StatusTriaged},
	StatusVerified:        {StatusNeedsReview, StatusClosed},
	StatusNeedsReview:     {StatusClosed, StatusCancelled},
	StatusFailed:          {StatusTriaged},
	StatusCancelled:       {StatusTriaged},
	StatusClosed:          {StatusRunning},
}

type Issue struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	UserID      string     `json:"userId"`
	AssistantID string     `json:"assistantId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	ProjectID   string     `json:"projectId,omitempty"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	PRURL       string     `json:"prUrl,omitempty"`
	Status      string     `json:"status"`
	Origin      string     `json:"origin"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`

	// Anti-thrash / claim lease (Kanban).
	DiscussionRound int        `json:"discussionRound"`
	ReviewRound     int        `json:"reviewRound"`
	ClaimOwner      string     `json:"claimOwner,omitempty"`
	ClaimedAt       *time.Time `json:"claimedAt,omitempty"`
	LastStatusFrom  string     `json:"lastStatusFrom,omitempty"`
	LastEvidence    string     `json:"lastEvidence,omitempty"`
	LastBoardScanAt *time.Time `json:"lastBoardScanAt,omitempty"`
}

// Transition is one audited status change.
type Transition struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issueId"`
	UserID    string    `json:"userId"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	ActorKind string    `json:"actorKind"`
	ActorID   string    `json:"actorId,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Evidence  string    `json:"evidence,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
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

// CanTransition reports whether from→to is an allowed board edge.
func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, next := range allowedTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// AllowedTargets returns the legal next statuses from from (excluding no-op).
func AllowedTargets(from string) []string {
	src := allowedTransitions[from]
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// ErrInvalidTransition is returned when a status change violates the board edges.
type ErrInvalidTransition struct {
	From, To string
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid transition %s → %s", e.From, e.To)
}

// Advance returns the issue status after ev. Forward-only within the Kanban
// machine: a rule that does not apply leaves the status unchanged, and a
// cancelled issue never auto-revives (reopen is an explicit transition).
func Advance(cur string, ev Event) string {
	if cur == StatusCancelled {
		return cur
	}
	switch ev {
	case EventSpecCurrent:
		if cur == StatusNew {
			return StatusSpecReview
		}
	case EventPlanCurrent:
		switch cur {
		case StatusNew, StatusSpecReview, StatusNeedsDiscussion:
			return StatusTriaged
		}
	case EventTaskOpen:
		switch cur {
		case StatusClosed:
			return StatusRunning
		case StatusTriaged, StatusClaimed, StatusVerified, StatusNeedsReview, StatusFailed:
			return StatusRunning
		case StatusRunning:
			return StatusRunning
		case StatusNew, StatusSpecReview, StatusNeedsDiscussion:
			// Work started before triage: jump onto the execution lane.
			return StatusRunning
		}
	case EventAllTasksDone:
		switch cur {
		case StatusClosed, StatusCancelled:
			return cur
		default:
			return StatusVerified
		}
	}
	return cur
}

// IsClosedStatus reports whether status should stamp closed_at.
func IsClosedStatus(status string) bool {
	return status == StatusClosed || status == StatusCancelled
}

// IsOpenBoardStatus is true for columns the board scanner / proactive digest care about.
func IsOpenBoardStatus(status string) bool {
	switch status {
	case StatusNew, StatusSpecReview, StatusNeedsDiscussion, StatusTriaged,
		StatusClaimed, StatusRunning, StatusVerified, StatusNeedsReview, StatusFailed:
		return true
	}
	return false
}

// OpenBoardStatuses lists non-terminal statuses for scanners.
func OpenBoardStatuses() []string {
	out := make([]string, 0, len(BoardColumnOrder))
	for _, s := range BoardColumnOrder {
		if IsOpenBoardStatus(s) {
			out = append(out, s)
		}
	}
	return out
}

// AllTasksDone reports whether the task list satisfies the completion rule:
// at least one done, and nothing left in todo/in_progress/blocked. An issue whose
// tasks were all cancelled stays open for the user to decide.
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
	case StatusNew, StatusSpecReview, StatusNeedsDiscussion, StatusTriaged,
		StatusClaimed, StatusRunning, StatusVerified, StatusNeedsReview,
		StatusFailed, StatusClosed, StatusCancelled:
		return nil
	}
	return fmt.Errorf("invalid issue status %q", status)
}
