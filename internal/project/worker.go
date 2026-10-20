package project

import (
	"fmt"
	"strings"
	"time"
)

const (
	WorkerQueued    = "queued"
	WorkerRunning   = "running"
	WorkerSucceeded = "succeeded"
	WorkerFailed    = "failed"
	WorkerCancelled = "cancelled"

	RoleImplementer = "implementer"
	RoleReviewer    = "reviewer"
	RoleTester      = "tester"
)

// Worker is one delegated execution unit inside a Project.
type Worker struct {
	ID           string        `json:"id"`
	Key          string        `json:"key"`
	ProjectID    string        `json:"projectId"`
	UserID       string        `json:"userId"`
	SessionID    string        `json:"sessionId,omitempty"`
	IssueID      string        `json:"issueId,omitempty"`
	Title        string        `json:"title"`
	Brief        string        `json:"brief"`
	Role         string        `json:"role"`
	Status       string        `json:"status"`
	Error        string        `json:"error,omitempty"`
	ReviewJSON   string        `json:"-"`
	Review       *ReviewResult `json:"review,omitempty"`
	UserDecision string        `json:"userDecision,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	StartedAt    *time.Time    `json:"startedAt,omitempty"`
	FinishedAt   *time.Time    `json:"finishedAt,omitempty"`
}

type DelegateInput struct {
	Title   string
	Brief   string
	Role    string
	IssueID string // optional resolved issue id
}

// NormalizeWorkerRole returns a valid code-dev role (default implementer).
func NormalizeWorkerRole(role string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "", RoleImplementer:
		return RoleImplementer, nil
	case RoleReviewer:
		return RoleReviewer, nil
	case RoleTester:
		return RoleTester, nil
	default:
		return "", fmt.Errorf("role must be implementer, reviewer, or tester")
	}
}

func ValidateWorkerTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	if len([]rune(strings.TrimSpace(title))) > 200 {
		return fmt.Errorf("title is too long")
	}
	return nil
}

func ValidateWorkerStatus(status string) error {
	switch status {
	case WorkerQueued, WorkerRunning, WorkerSucceeded, WorkerFailed, WorkerCancelled:
		return nil
	default:
		return fmt.Errorf("invalid worker status")
	}
}

// RenderWorkerPrompt is the first user turn for a delegated worker.
func RenderWorkerPrompt(p *Project, w *Worker) string {
	var b strings.Builder
	role := w.Role
	if role == "" {
		role = RoleImplementer
	}
	switch role {
	case RoleReviewer:
		b.WriteString("Review assignment — do not implement the feature.\n\n")
	case RoleTester:
		b.WriteString("Verification assignment — reproduce / run tests / report pass-fail.\n\n")
	default:
		b.WriteString("Implementation assignment — make the code change described below.\n\n")
	}
	if p != nil {
		b.WriteString("Project: ")
		b.WriteString(p.Name)
		b.WriteString(" (")
		b.WriteString(p.Key)
		b.WriteString(")\n")
		b.WriteString("Role: ")
		b.WriteString(role)
		b.WriteString("\n")
		b.WriteString("Repo path (under /workspace): ")
		b.WriteString(p.RepoPath)
		b.WriteString("\n")
		b.WriteString("Shared context: ")
		b.WriteString(p.RepoPath)
		b.WriteString("/.roundpen/context\n")
		if p.RepoURL != "" {
			b.WriteString("Remote (optional): ")
			b.WriteString(p.RepoURL)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Task: ")
	b.WriteString(w.Title)
	b.WriteString("\n")
	if strings.TrimSpace(w.Brief) != "" {
		b.WriteString("\nBrief:\n")
		b.WriteString(strings.TrimSpace(w.Brief))
		b.WriteString("\n")
	}
	b.WriteString("\nBefore you start, skim Shared context (README.md and any *.md notes) for conventions and how to test.\n")
	b.WriteString("If you learn durable facts (build/test commands, style prefs), append a short note there for later workers.\n")
	switch role {
	case RoleReviewer:
		b.WriteString("\nInspect the relevant diffs and paths. Return severity-ordered findings and a verdict.\n")
		b.WriteString("End your final message with a fenced JSON block (and nothing after it) using this shape:\n")
		b.WriteString("```json\n")
		b.WriteString(`{"verdict":"approve"|"request_changes","summary":"one sentence","findings":[{"severity":"error"|"warning"|"info","path":"relative/path","line":0,"note":"concrete issue"}]}`)
		b.WriteString("\n```\n")
		b.WriteString("Remote PR review threads stay on GitHub; this JSON is for the Roundpen console only.\n")
	case RoleTester:
		b.WriteString("\nRun the verification steps. Report commands, expected vs actual, and pass/fail.\n")
	default:
		b.WriteString("\nWork in the repo path. When done, summarize what changed and how to verify it.\n")
	}
	return b.String()
}

// RenderWorkerDoneNotice is written into the coordinator transcript when a worker finishes.
func RenderWorkerDoneNotice(w *Worker, status string) string {
	if w == nil {
		return "[System] A project worker finished."
	}
	role := w.Role
	if role == "" {
		role = RoleImplementer
	}
	var b strings.Builder
	b.WriteString("[System] Worker ")
	b.WriteString(w.Key)
	b.WriteString(" (")
	b.WriteString(role)
	b.WriteString(") finished with status ")
	b.WriteString(status)
	b.WriteString(".\nTask: ")
	b.WriteString(w.Title)
	if strings.TrimSpace(w.Error) != "" {
		b.WriteString("\nError: ")
		b.WriteString(strings.TrimSpace(w.Error))
	}
	if w.Review != nil {
		b.WriteString("\nReviewer verdict: ")
		b.WriteString(w.Review.Verdict)
		if strings.TrimSpace(w.Review.Summary) != "" {
			b.WriteString(" — ")
			b.WriteString(strings.TrimSpace(w.Review.Summary))
		}
		b.WriteString(".")
	}
	b.WriteString("\nSummarize for the user, or ListProjectWorkers / open the worker session for details. Delegate follow-ups if needed.")
	b.WriteString("\nRemote PR discussion (if any) stays on GitHub; use the project page Pass / Request changes controls for local acceptance.")
	return b.String()
}
