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
)

// Worker is one delegated execution unit inside a Project.
type Worker struct {
	ID         string     `json:"id"`
	Key        string     `json:"key"`
	ProjectID  string     `json:"projectId"`
	UserID     string     `json:"userId"`
	SessionID  string     `json:"sessionId,omitempty"`
	IssueID    string     `json:"issueId,omitempty"`
	Title      string     `json:"title"`
	Brief      string     `json:"brief"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type DelegateInput struct {
	Title   string
	Brief   string
	IssueID string // optional resolved issue id
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
	b.WriteString("You are a project worker agent. Implement the task below; do not replan the whole project.\n\n")
	if p != nil {
		b.WriteString("Project: ")
		b.WriteString(p.Name)
		b.WriteString(" (")
		b.WriteString(p.Key)
		b.WriteString(")\n")
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
	b.WriteString("\nWork in the repo path. When done, summarize what changed and how to verify it.\n")
	return b.String()
}
