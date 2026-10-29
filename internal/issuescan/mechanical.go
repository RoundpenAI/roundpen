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

// DefaultIssueScanCooldown skips re-mentioning the same issue in board digests.
const DefaultIssueScanCooldown = 12 * time.Hour

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

// RequeueStaleClaims moves claimed issues whose lease expired back to triaged.
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
		if !issue.ClaimLeaseExpired(&it, now, lease) {
			continue
		}
		a := Action{
			Key: it.Key, From: it.Status, To: issue.StatusTriaged,
			Reason: fmt.Sprintf("claim lease expired after %s", lease),
		}
		_, err := store.TransitionWith(ctx, userID, it.Key, issue.TransitionRequest{
			To: issue.StatusTriaged, ActorKind: issue.ActorSystem, ActorID: "system",
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
