// Package routine is the standing-task model: a recurring assignment to one
// assistant, woken by the control plane rather than by a chat message.
package routine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	AutonomyRead   = "read"
	AutonomyBrowse = "browse"

	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusArchived = "archived"

	RunQueued         = "queued"
	RunRunning        = "running"
	RunWaitingUser    = "waiting_user"
	RunSucceeded      = "succeeded"
	RunFailed         = "failed"
	RunSkippedOverlap = "skipped_overlap"
	RunSkippedStale   = "skipped_stale"
	RunCancelled      = "cancelled"

	DueFire  = "fire"
	DueStale = "stale"

	MaxStateBytes      = 64 << 10
	DefaultMaxDuration = 900
	MinDurationSec     = 60
	MaxDurationSec     = 3600

	minGap         = time.Minute
	day            = 24 * time.Hour
	gapSampleFires = 48
)

var (
	ErrNotFound  = errors.New("routine not found")
	ErrInvalid   = errors.New("invalid routine")
	ErrForbidden = errors.New("forbidden")
)

// Schedule is a five-field cron expression evaluated in a fixed time zone.
type Schedule struct {
	Expr string
	Loc  *time.Location
	spec cron.Schedule
}

// NextAfter returns the first fire strictly after from, in UTC.
func (s Schedule) NextAfter(from time.Time) time.Time {
	local := from.In(s.Loc)
	return s.spec.Next(local).UTC()
}

// Interval is the gap between the next two fires after from.
func (s Schedule) Interval(from time.Time) time.Duration {
	a := s.NextAfter(from)
	return s.NextAfter(a).Sub(a)
}

// ParseSchedule parses a five-field cron expression in an IANA time zone and
// rejects schedules that can fire more often than once a minute.
func ParseSchedule(expr, tz string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if len(strings.Fields(expr)) != 5 {
		return Schedule{}, fmt.Errorf("%w: cron must be five fields", ErrInvalid)
	}
	tz = strings.TrimSpace(tz)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: timezone: %v", ErrInvalid, err)
	}
	spec, err := cron.ParseStandard(expr)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: cron: %v", ErrInvalid, err)
	}
	s := Schedule{Expr: expr, Loc: loc, spec: spec}
	if err := checkMinGap(s.NextAfter, time.Now()); err != nil {
		return Schedule{}, err
	}
	return s, nil
}

// checkMinGap rejects a clock whose consecutive fires are under a minute.
// n fires produce n-1 gaps; gapSampleFires is the sample size.
func checkMinGap(next func(time.Time) time.Time, from time.Time) error {
	t := next(from)
	for i := 1; i < gapSampleFires; i++ {
		n := next(t)
		if n.Sub(t) < minGap {
			return fmt.Errorf("%w: schedule interval must be at least 1 minute", ErrInvalid)
		}
		t = n
	}
	return nil
}

// GraceFor is how late a fire may be and still run once.
// A daily-or-slower schedule waits up to a day; a faster one waits one period.
func GraceFor(interval time.Duration) time.Duration {
	if interval >= day {
		return day
	}
	return interval
}

// ClassifyDue reports whether a scheduled instant should still run.
func ClassifyDue(scheduled, now time.Time, grace time.Duration) string {
	if now.Sub(scheduled) <= grace {
		return DueFire
	}
	return DueStale
}

// Artifact is a path in the workspace or a markdown blob attached to a run.
type Artifact struct {
	Path     string `json:"path,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

// Routine is one standing assignment.
type Routine struct {
	ID                   string          `json:"id"`
	Key                  string          `json:"key"`
	UserID               string          `json:"userId"`
	AssigneeAssistantID  string          `json:"assigneeAssistantId,omitempty"`
	CreatedByAssistantID string          `json:"createdByAssistantId,omitempty"`
	CreatedBySessionID   string          `json:"createdBySessionId,omitempty"`
	IssueID              string          `json:"issueId,omitempty"`
	Title                string          `json:"title"`
	Brief                string          `json:"brief"`
	Autonomy             string          `json:"autonomy"`
	Hosts                []string        `json:"hosts"`
	Cron                 string          `json:"cron"`
	Timezone             string          `json:"timezone"`
	DeliverIM            bool            `json:"deliverIm"`
	MaxDurationSec       int             `json:"maxDurationSec"`
	State                json.RawMessage `json:"state"`
	Status               string          `json:"status"`
	NextRunAt            *time.Time      `json:"nextRunAt,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
	// LastSummary is filled on list responses. It is not a stored column.
	LastSummary string `json:"lastSummary,omitempty"`
}

// Run is one firing of a routine.
type Run struct {
	ID             string          `json:"id"`
	Key            string          `json:"key"`
	RoutineID      string          `json:"routineId"`
	UserID         string          `json:"userId"`
	AssistantID    string          `json:"assistantId,omitempty"`
	SessionID      string          `json:"sessionId,omitempty"`
	Status         string          `json:"status"`
	ScheduledAt    time.Time       `json:"scheduledAt"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	FinishedAt     *time.Time      `json:"finishedAt,omitempty"`
	WaitStartedAt  *time.Time      `json:"waitStartedAt,omitempty"`
	Summary        string          `json:"summary"`
	Artifacts      json.RawMessage `json:"artifacts,omitempty"`
	Error          string          `json:"error,omitempty"`
	DeliverError   string          `json:"deliverError,omitempty"`
	AssistTicketID string          `json:"assistTicketId,omitempty"`
	BudgetLeftSec  *int            `json:"budgetLeftSec,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// CreateInput is a validated new routine. Hosts and MaxDurationSec are
// normalized by ValidateCreate.
type CreateInput struct {
	AssigneeAssistantID  string
	CreatedByAssistantID string
	CreatedBySessionID   string
	IssueID              string
	Title                string
	Brief                string
	Autonomy             string
	Hosts                []string
	Cron                 string
	Timezone             string
	DeliverIM            bool
	MaxDurationSec       int
}

// ValidateCreate checks fields that do not depend on the assignee's constitution.
// Capability checks (browser, network) stay in the HTTP layer.
func ValidateCreate(in *CreateInput) (Schedule, error) {
	if in == nil {
		return Schedule{}, fmt.Errorf("%w: missing input", ErrInvalid)
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return Schedule{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	in.Brief = strings.TrimSpace(in.Brief)
	switch in.Autonomy {
	case AutonomyRead, AutonomyBrowse:
	default:
		return Schedule{}, fmt.Errorf("%w: autonomy must be read or browse", ErrInvalid)
	}
	hosts, err := normalizeHosts(in.Hosts)
	if err != nil {
		return Schedule{}, err
	}
	if in.Autonomy == AutonomyBrowse && len(hosts) == 0 {
		return Schedule{}, fmt.Errorf("%w: browse requires at least one host", ErrInvalid)
	}
	if in.Autonomy == AutonomyRead && len(hosts) > 0 {
		return Schedule{}, fmt.Errorf("%w: read does not take a host list", ErrInvalid)
	}
	in.Hosts = hosts
	if in.MaxDurationSec == 0 {
		in.MaxDurationSec = DefaultMaxDuration
	}
	if in.MaxDurationSec < MinDurationSec || in.MaxDurationSec > MaxDurationSec {
		return Schedule{}, fmt.Errorf("%w: max duration must be between %d and %d seconds", ErrInvalid, MinDurationSec, MaxDurationSec)
	}
	return ParseSchedule(in.Cron, in.Timezone)
}

func normalizeHosts(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, h := range in {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if strings.Contains(h, "://") || strings.ContainsAny(h, "/:?#") {
			return nil, fmt.Errorf("%w: host %q must be a bare hostname", ErrInvalid, h)
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out, nil
}

// NormalizeState checks that state is a JSON object within the size cap.
func NormalizeState(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > MaxStateBytes {
		return nil, fmt.Errorf("%w: state exceeds %d bytes", ErrInvalid, MaxStateBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: state must be a JSON object", ErrInvalid)
	}
	return raw, nil
}

func validateStatus(status string) error {
	switch status {
	case StatusActive, StatusPaused, StatusArchived:
		return nil
	default:
		return fmt.Errorf("%w: status must be active, paused, or archived", ErrInvalid)
	}
}
