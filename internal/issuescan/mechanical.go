package issuescan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

// DefaultClaimLease is how long a claimed issue may sit before requeue.
const DefaultClaimLease = 2 * time.Hour

// IssueStore is the subset of issue.Store the scanner needs.
type IssueStore interface {
	List(ctx context.Context, userID string, f issue.ListFilter) ([]issue.Issue, error)
	Transition(ctx context.Context, userID, keyOrID, to string) (*issue.Issue, error)
}

// Action is one mechanical or suggested follow-up on the board.
type Action struct {
	Key    string `json:"key"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	Did    bool   `json:"did"` // true when the scanner applied the transition
}

// RequeueStaleClaims moves claimed issues whose updated_at is older than lease back to triaged.
func RequeueStaleClaims(ctx context.Context, store IssueStore, userID string, now time.Time, lease time.Duration) ([]Action, error) {
	if store == nil {
		return nil, nil
	}
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	rows, err := store.List(ctx, userID, issue.ListFilter{Status: issue.StatusClaimed, Limit: 100})
	if err != nil {
		return nil, err
	}
	var out []Action
	for _, it := range rows {
		if now.Sub(it.UpdatedAt) < lease {
			continue
		}
		a := Action{
			Key: it.Key, From: it.Status, To: issue.StatusTriaged,
			Reason: fmt.Sprintf("claim lease expired after %s", lease),
		}
		if _, err := store.Transition(ctx, userID, it.Key, issue.StatusTriaged); err != nil {
			a.Reason = a.Reason + ": " + err.Error()
			out = append(out, a)
			continue
		}
		a.Did = true
		out = append(out, a)
	}
	return out, nil
}

// BoardDigest summarizes open columns for a sysadmin prompt.
func BoardDigest(ctx context.Context, store IssueStore, userID string, limitPerCol int) (string, string, error) {
	if store == nil {
		return "", "", nil
	}
	if limitPerCol <= 0 {
		limitPerCol = 8
	}
	var b strings.Builder
	b.WriteString("事项看板巡检（少打扰）：请用 ListIssueBoard 核对，仅对确有把握的事项调用 TransitionIssue；")
	b.WriteString("有 Project 时可用 DelegateProjectWork / ListExperts→DelegateToExpert 委派，不要刷屏用户。\n")
	hashParts := make([]string, 0, 32)
	total := 0
	for _, st := range issue.OpenBoardStatuses() {
		rows, err := store.List(ctx, userID, issue.ListFilter{Status: st, Limit: limitPerCol})
		if err != nil {
			return "", "", err
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s (%d)\n", st, len(rows))
		for _, it := range rows {
			total++
			line := it.Key + " · " + strings.TrimSpace(it.Title)
			if it.PRURL != "" {
				line += " · PR " + it.PRURL
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteByte('\n')
			hashParts = append(hashParts, it.Key+":"+it.Status+":"+it.UpdatedAt.UTC().Format(time.RFC3339))
		}
	}
	if total == 0 {
		return "", "", nil
	}
	return b.String(), strings.Join(hashParts, "|"), nil
}
