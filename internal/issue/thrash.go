package issue

import (
	"fmt"
	"strings"
	"time"
)

// Anti-thrash defaults (少配置；常量即可).
const (
	MaxDiscussionRounds = 3
	MaxReviewRounds     = 2
	MinAgentReasonLen   = 8
)

// Actor kinds for transitions.
const (
	ActorHuman    = "human"
	ActorAgent    = "agent"
	ActorSysadmin = "sysadmin"
	ActorSystem   = "system"
)

// TransitionRequest is an explicit board move with anti-thrash metadata.
type TransitionRequest struct {
	To          string
	Reason      string
	Evidence    string
	ActorKind   string // human|agent|sysadmin|system
	ActorID     string // assistant id, username, or "system"
	Force       bool   // human-only override
	ClearOwner  bool   // set by policy when leaving claim lane
	ForceTo     string // non-empty when policy rewrites destination (e.g. forced triage)
}

// ErrThrash is returned when anti-thrash rules block a move.
type ErrThrash struct {
	Code    string
	Message string
}

func (e ErrThrash) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// PolicyResult is the outcome of CheckTransitionPolicy before the write.
type PolicyResult struct {
	To             string
	DiscussionInc  bool
	ReviewInc      bool
	SetClaimOwner  string // non-empty → set; "-" → clear
	ForcedTriaged  bool
	BreakHysteresis bool
}

func isDiscussionPair(a, b string) bool {
	return (a == StatusSpecReview && b == StatusNeedsDiscussion) ||
		(a == StatusNeedsDiscussion && b == StatusSpecReview)
}

func isHuman(kind string) bool {
	return kind == ActorHuman || kind == ""
}

func isPrivileged(kind string) bool {
	return isHuman(kind) || kind == ActorSysadmin || kind == ActorSystem
}

// CheckTransitionPolicy validates thrash / ownership on top of CanTransition.
// Callers must already ensure CanTransition(from, req.To) (or accept ForceTo).
func CheckTransitionPolicy(it *Issue, req TransitionRequest) (PolicyResult, error) {
	out := PolicyResult{To: req.To}
	if it == nil {
		return out, fmt.Errorf("issue is required")
	}
	kind := strings.TrimSpace(req.ActorKind)
	if kind == "" {
		kind = ActorHuman
	}
	actorID := strings.TrimSpace(req.ActorID)
	to := strings.TrimSpace(req.To)
	reason := strings.TrimSpace(req.Reason)
	evidence := strings.TrimSpace(req.Evidence)
	force := req.Force && isHuman(kind)

	if to == "" {
		return out, fmt.Errorf("to is required")
	}
	if err := ValidateIssueStatus(to); err != nil {
		return out, err
	}

	// Agent / sysadmin must explain themselves (system mechanical exempt).
	if (kind == ActorAgent || kind == ActorSysadmin) && !force {
		if len([]rune(reason)) < MinAgentReasonLen {
			return out, ErrThrash{
				Code: "reason_required",
				Message: fmt.Sprintf(
					"agent/sysadmin transitions need a reason (≥%d characters); refuse status ping-pong without rationale",
					MinAgentReasonLen),
			}
		}
	}

	// Single owner while claimed / running.
	if it.Status == StatusClaimed || it.Status == StatusRunning {
		owner := strings.TrimSpace(it.ClaimOwner)
		if owner != "" && !force && kind != ActorSystem && kind != ActorSysadmin && !isHuman(kind) {
			if kind != ActorAgent || actorID == "" || actorID != owner {
				return out, ErrThrash{
					Code: "not_claim_owner",
					Message: fmt.Sprintf(
						"issue %s is claimed by %s; only that agent, sysadmin, or a human may transition it (no parallel fighting)",
						it.Key, owner),
				}
			}
		}
	}

	// Claiming: only one owner; agents must identify.
	if it.Status == StatusTriaged && to == StatusClaimed {
		if kind == ActorAgent && actorID == "" && !force {
			return out, ErrThrash{
				Code:    "claim_identity_required",
				Message: "agents must pass assistantId when claiming an issue",
			}
		}
		if kind == ActorAgent {
			out.SetClaimOwner = actorID
		} else if isHuman(kind) {
			if actorID != "" {
				out.SetClaimOwner = actorID
			} else {
				out.SetClaimOwner = "human"
			}
		} else if kind == ActorSysadmin {
			out.SetClaimOwner = actorID
			if out.SetClaimOwner == "" {
				out.SetClaimOwner = "sysadmin"
			}
		}
	}

	// Discussion round cap → forced triage.
	if isDiscussionPair(it.Status, to) && !force {
		if it.DiscussionRound >= MaxDiscussionRounds {
			if to != StatusTriaged {
				out.To = StatusTriaged
				out.ForcedTriaged = true
				// Still need edge from current → triaged.
				if !CanTransition(it.Status, StatusTriaged) {
					return out, ErrThrash{
						Code: "discussion_round_limit",
						Message: fmt.Sprintf(
							"discussion_round=%d reached limit %d; cannot bounce between spec_review and needs_discussion — promote to triaged or ask a human",
							it.DiscussionRound, MaxDiscussionRounds),
					}
				}
			}
		} else {
			out.DiscussionInc = true
		}
	}
	// After forced rewrite, verify edge for rewritten target.
	if out.ForcedTriaged {
		to = out.To
	}

	// Review round cap: block re-entering needs_review.
	if to == StatusNeedsReview && !force {
		if it.ReviewRound >= MaxReviewRounds {
			return out, ErrThrash{
				Code: "review_round_limit",
				Message: fmt.Sprintf(
					"review_round=%d reached limit %d; do not re-enter needs_review — close, cancel, or ask a human",
					it.ReviewRound, MaxReviewRounds),
			}
		}
		out.ReviewInc = true
	}

	// Hysteresis: forbid immediate reverse without new evidence.
	if !force && kind != ActorSystem && !isHuman(kind) {
		lastFrom := strings.TrimSpace(it.LastStatusFrom)
		if lastFrom != "" && to == lastFrom {
			prevEvidence := strings.TrimSpace(it.LastEvidence)
			if evidence == "" || evidence == prevEvidence {
				return out, ErrThrash{
					Code: "hysteresis",
					Message: fmt.Sprintf(
						"refusing flip-flop back to %s without new evidence (or a human promote); last move was %s → %s",
						to, lastFrom, it.Status),
				}
			}
			out.BreakHysteresis = true
		}
	}

	if !CanTransition(it.Status, to) {
		return out, ErrInvalidTransition{From: it.Status, To: to}
	}

	// Clear claim when leaving the execution lane.
	switch to {
	case StatusTriaged, StatusFailed, StatusClosed, StatusCancelled, StatusVerified, StatusNeedsReview, StatusNew, StatusSpecReview, StatusNeedsDiscussion:
		if to != StatusClaimed && to != StatusRunning {
			out.SetClaimOwner = "-"
		}
	}
	// Keep owner on claimed → running.
	if it.Status == StatusClaimed && to == StatusRunning {
		out.SetClaimOwner = "" // leave unchanged
	}

	out.To = to
	return out, nil
}

// ClaimLeaseExpired reports whether a claimed issue should return to triaged.
func ClaimLeaseExpired(it *Issue, now time.Time, lease time.Duration) bool {
	if it == nil || it.Status != StatusClaimed {
		return false
	}
	start := it.ClaimedAt
	if start == nil {
		start = &it.UpdatedAt
	}
	if lease <= 0 {
		lease = 2 * time.Hour
	}
	return now.Sub(start.UTC()) >= lease
}
