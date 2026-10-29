package issue

import (
	"strings"
	"testing"
	"time"
)

func TestCheckTransitionPolicy_DiscussionCapForcesTriaged(t *testing.T) {
	it := &Issue{Key: "ISS-1", Status: StatusNeedsDiscussion, DiscussionRound: MaxDiscussionRounds}
	res, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusSpecReview, ActorKind: ActorAgent, ActorID: "asst-1",
		Reason: "still unclear about scope boundaries here",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.ForcedTriaged || res.To != StatusTriaged {
		t.Fatalf("want forced triaged, got %+v", res)
	}
}

func TestCheckTransitionPolicy_Hysteresis(t *testing.T) {
	it := &Issue{
		Key: "ISS-1", Status: StatusNeedsDiscussion, LastStatusFrom: StatusSpecReview,
		DiscussionRound: 1,
	}
	_, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusSpecReview, ActorKind: ActorAgent, ActorID: "asst-1",
		Reason: "going back without new facts at all",
	})
	if err == nil || !strings.Contains(err.Error(), "hysteresis") {
		t.Fatalf("want hysteresis, got %v", err)
	}
	_, err = CheckTransitionPolicy(it, TransitionRequest{
		To: StatusSpecReview, ActorKind: ActorAgent, ActorID: "asst-1",
		Reason: "user clarified API shape in chat", Evidence: "user:use-rest",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckTransitionPolicy_ClaimOwner(t *testing.T) {
	it := &Issue{Key: "ISS-1", Status: StatusRunning, ClaimOwner: "asst-a"}
	_, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusNeedsReview, ActorKind: ActorAgent, ActorID: "asst-b",
		Reason: "I want to take over this work now",
	})
	if err == nil || !strings.Contains(err.Error(), "not_claim_owner") {
		t.Fatalf("want not_claim_owner, got %v", err)
	}
	_, err = CheckTransitionPolicy(it, TransitionRequest{
		To: StatusNeedsReview, ActorKind: ActorAgent, ActorID: "asst-a",
		Reason: "delivery failed conflict on main",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckTransitionPolicy_ReasonRequired(t *testing.T) {
	it := &Issue{Key: "ISS-1", Status: StatusTriaged}
	_, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusClaimed, ActorKind: ActorAgent, ActorID: "asst-1", Reason: "short",
	})
	if err == nil || !strings.Contains(err.Error(), "reason_required") {
		t.Fatalf("want reason_required, got %v", err)
	}
}

func TestCheckTransitionPolicy_HumanBypassesHysteresis(t *testing.T) {
	it := &Issue{Key: "ISS-1", Status: StatusNeedsDiscussion, LastStatusFrom: StatusSpecReview}
	_, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusSpecReview, ActorKind: ActorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckTransitionPolicy_ReviewCap(t *testing.T) {
	it := &Issue{Key: "ISS-1", Status: StatusRunning, ClaimOwner: "asst-a", ReviewRound: MaxReviewRounds}
	_, err := CheckTransitionPolicy(it, TransitionRequest{
		To: StatusNeedsReview, ActorKind: ActorAgent, ActorID: "asst-a",
		Reason: "another review pass please",
	})
	if err == nil || !strings.Contains(err.Error(), "review_round_limit") {
		t.Fatalf("want review_round_limit, got %v", err)
	}
}

func TestClaimLeaseExpired(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	past := now.Add(-3 * time.Hour)
	it := &Issue{Status: StatusClaimed, ClaimedAt: &past}
	if !ClaimLeaseExpired(it, now, time.Hour) {
		t.Fatal("expected expired")
	}
}
