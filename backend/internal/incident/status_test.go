package incident

import "testing"

func TestValidStatus(t *testing.T) {
	valid := []string{
		"pending_investigation",
		"investigating",
		"awaiting_review",
		"approved",
		"broadcasted",
		"rejected",
		"failed",
		"closed",
	}
	for _, status := range valid {
		if !ValidStatus(status) {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	if ValidStatus("sent") {
		t.Fatal("expected unknown status to be invalid")
	}
}

func TestCanTransition(t *testing.T) {
	allowed := []struct {
		from Status
		to   Status
	}{
		{StatusPendingInvestigation, StatusInvestigating},
		{StatusInvestigating, StatusAwaitingReview},
		{StatusAwaitingReview, StatusApproved},
		{StatusAwaitingReview, StatusRejected},
		{StatusApproved, StatusBroadcasted},
		{StatusFailed, StatusPendingInvestigation},
		{StatusBroadcasted, StatusClosed},
	}
	for _, transition := range allowed {
		if !CanTransition(transition.from, transition.to) {
			t.Fatalf("expected %s -> %s to be allowed", transition.from, transition.to)
		}
	}
	if CanTransition(StatusPendingInvestigation, StatusBroadcasted) {
		t.Fatal("pending incident must not jump directly to broadcasted")
	}
}
