package issuescan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

// Non-terminal issue statuses must age out — board scan applies these leases.
const (
	// DefaultClaimLease is how long a claimed issue may sit before requeue.
	DefaultClaimLease = 2 * time.Hour
	// DefaultRunningLease is how long running may sit without a status change.
	DefaultRunningLease = 2 * time.Hour
	// DefaultReviewIdle auto-closes verified / needs_review after idle.
	DefaultReviewIdle = 7 * 24 * time.Hour
	// DefaultFailedIdle auto-cancels failed issues left unrequeued.
	DefaultFailedIdle = 14 * 24 * time.Hour
	// DefaultBacklogIdle auto-cancels untouched backlog columns.
	DefaultBacklogIdle = 30 * 24 * time.Hour
	// DefaultIssueScanCooldown skips re-mentioning the same issue in board digests.
	DefaultIssueScanCooldown = 12 * time.Hour
)

// IssueStore is the subset of issue.Store the scanner needs.
type IssueStore interface {
	List(ctx context.Context, userID string, f issue.ListFilter) ([]issue.Issue, error)
	TransitionWith(ctx context.Context, userID, keyOrID string, req issue.TransitionRequest) (*issue.Issue, error)
	MarkBoardScanned(ctx context.Context, userID string, keys []string, at time.Time) error
}

// Action is one mechanical or suggested follow-up on the board.
type Action struct {
	Key    string `json:"key"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	Did    bool   `json:"did"` // true when the scanner applied the transition
}

// TimeoutPolicy holds leases for ApplyStatusTimeouts. Zero fields use defaults.
type TimeoutPolicy struct {
	ClaimLease   time.Duration
	RunningLease time.Duration
	ReviewIdle   time.Duration
	FailedIdle   time.Duration
	BacklogIdle  time.Duration
}

func (p TimeoutPolicy) withDefaults() TimeoutPolicy {
	if p.ClaimLease <= 0 {
		p.ClaimLease = DefaultClaimLease
	}
	if p.RunningLease <= 0 {
		p.RunningLease = DefaultRunningLease
	}
	if p.ReviewIdle <= 0 {
		p.ReviewIdle = DefaultReviewIdle
	}
	if p.FailedIdle <= 0 {
		p.FailedIdle = DefaultFailedIdle
	}
	if p.BacklogIdle <= 0 {
		p.BacklogIdle = DefaultBacklogIdle
	}
	return p
}

// RequeueStaleClaims moves claimed issues whose lease expired back to triaged.
func RequeueStaleClaims(ctx context.Context, store IssueStore, userID string, now time.Time, lease time.Duration) ([]Action, error) {
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	return applyTimeoutRules(ctx, store, userID, now, []timeoutRule{
		claimTimeoutRule(lease),
	})
}

// ApplyStatusTimeouts transitions non-terminal issues that outlived their lease
// so the board cannot wedge on claimed/running/review/backlog forever.
func ApplyStatusTimeouts(ctx context.Context, store IssueStore, userID string, now time.Time, pol TimeoutPolicy) ([]Action, error) {
	pol = pol.withDefaults()
	return applyTimeoutRules(ctx, store, userID, now, []timeoutRule{
		claimTimeoutRule(pol.ClaimLease),
		{issue.StatusRunning, issue.StatusFailed, pol.RunningLease, "running lease expired after %s", updatedAge},
		{issue.StatusVerified, issue.StatusClosed, pol.ReviewIdle, "verified idle timeout after %s", updatedAge},
		{issue.StatusNeedsReview, issue.StatusClosed, pol.ReviewIdle, "needs_review idle timeout after %s", updatedAge},
		{issue.StatusFailed, issue.StatusCancelled, pol.FailedIdle, "failed idle timeout after %s", updatedAge},
		{issue.StatusNew, issue.StatusCancelled, pol.BacklogIdle, "backlog idle timeout after %s", updatedAge},
		{issue.StatusSpecReview, issue.StatusCancelled, pol.BacklogIdle, "backlog idle timeout after %s", updatedAge},
		{issue.StatusNeedsDiscussion, issue.StatusCancelled, pol.BacklogIdle, "backlog idle timeout after %s", updatedAge},
		{issue.StatusTriaged, issue.StatusCancelled, pol.BacklogIdle, "backlog idle timeout after %s", updatedAge},
	})
}

type timeoutRule struct {
	status string
	to     string
	lease  time.Duration
	reason string
	age    func(issue.Issue) time.Time
}

func claimedAge(it issue.Issue) time.Time {
	if it.ClaimedAt != nil {
		return it.ClaimedAt.UTC()
	}
	return it.UpdatedAt.UTC()
}

func updatedAge(it issue.Issue) time.Time { return it.UpdatedAt.UTC() }

func claimTimeoutRule(lease time.Duration) timeoutRule {
	return timeoutRule{
		issue.StatusClaimed, issue.StatusTriaged, lease,
		"claim lease expired after %s", claimedAge,
	}
}

func applyTimeoutRules(ctx context.Context, store IssueStore, userID string, now time.Time, rules []timeoutRule) ([]Action, error) {
	if store == nil {
		return nil, nil
	}
	now = now.UTC()
	var out []Action
	for _, rule := range rules {
		rows, err := store.List(ctx, userID, issue.ListFilter{Status: rule.status, Limit: 100})
		if err != nil {
			return out, err
		}
		for _, it := range rows {
			start := rule.age(it)
			if start.IsZero() || now.Sub(start) < rule.lease {
				continue
			}
			a := Action{
				Key: it.Key, From: it.Status, To: rule.to,
				Reason: fmt.Sprintf(rule.reason, rule.lease),
			}
			_, err := store.TransitionWith(ctx, userID, it.Key, issue.TransitionRequest{
				To: rule.to, ActorKind: issue.ActorSystem, ActorID: "system",
				Reason: a.Reason,
			})
			if err != nil {
				a.Reason = a.Reason + ": " + err.Error()
				out = append(out, a)
				continue
			}
			a.Did = true
			out = append(out, a)
		}
	}
	return out, nil
}

// BoardDigest summarizes open columns for a sysadmin prompt.
// Issues still inside scan cooldown are omitted (少打扰 / anti-thrash).
func BoardDigest(ctx context.Context, store IssueStore, userID string, now time.Time, limitPerCol int, cooldown time.Duration) (text, hash string, mentioned []string, err error) {
	if store == nil {
		return "", "", nil, nil
	}
	if limitPerCol <= 0 {
		limitPerCol = 8
	}
	if cooldown <= 0 {
		cooldown = DefaultIssueScanCooldown
	}
	var b strings.Builder
	b.WriteString("事项看板巡检（少打扰、防摇摆）：请用 ListIssueBoard 核对；流转必须 TransitionIssue 并写清 reason；")
	b.WriteString("已 claimed/running 的事项勿抢属主；讨论轮次触顶只能 triaged；有 Project 时用 DelegateProjectWork / ListExperts→DelegateToExpert。\n")
	hashParts := make([]string, 0, 32)
	mentioned = make([]string, 0, 16)
	total := 0
	for _, st := range issue.OpenBoardStatuses() {
		rows, err := store.List(ctx, userID, issue.ListFilter{Status: st, Limit: limitPerCol * 2})
		if err != nil {
			return "", "", nil, err
		}
		var shown []issue.Issue
		for _, it := range rows {
			if it.LastBoardScanAt != nil && now.Sub(it.LastBoardScanAt.UTC()) < cooldown {
				continue
			}
			shown = append(shown, it)
			if len(shown) >= limitPerCol {
				break
			}
		}
		if len(shown) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s (%d)\n", st, len(shown))
		for _, it := range shown {
			total++
			line := it.Key + " · " + strings.TrimSpace(it.Title)
			if it.ClaimOwner != "" {
				line += " · owner=" + it.ClaimOwner
			}
			if it.DiscussionRound > 0 {
				line += fmt.Sprintf(" · discussion_round=%d", it.DiscussionRound)
			}
			if it.PRURL != "" {
				line += " · PR " + it.PRURL
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteByte('\n')
			hashParts = append(hashParts, it.Key+":"+it.Status+":"+it.UpdatedAt.UTC().Format(time.RFC3339))
			mentioned = append(mentioned, it.Key)
		}
	}
	if total == 0 {
		return "", "", nil, nil
	}
	return b.String(), strings.Join(hashParts, "|"), mentioned, nil
}
