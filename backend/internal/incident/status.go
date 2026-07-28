package incident

type Status string

const (
	StatusPendingInvestigation Status = "pending_investigation"
	StatusInvestigating        Status = "investigating"
	StatusAwaitingReview       Status = "awaiting_review"
	StatusApproved             Status = "approved"
	StatusBroadcasted          Status = "broadcasted"
	StatusRejected             Status = "rejected"
	StatusFailed               Status = "failed"
	StatusClosed               Status = "closed"
)

func ValidStatus(value string) bool {
	switch Status(value) {
	case StatusPendingInvestigation, StatusInvestigating, StatusAwaitingReview, StatusApproved, StatusBroadcasted, StatusRejected, StatusFailed, StatusClosed:
		return true
	default:
		return false
	}
}

func CanTransition(from Status, to Status) bool {
	switch from {
	case StatusPendingInvestigation:
		return to == StatusInvestigating || to == StatusFailed || to == StatusClosed
	case StatusInvestigating:
		return to == StatusAwaitingReview || to == StatusFailed
	case StatusAwaitingReview:
		return to == StatusApproved || to == StatusRejected || to == StatusInvestigating || to == StatusClosed
	case StatusApproved:
		return to == StatusBroadcasted || to == StatusFailed
	case StatusBroadcasted, StatusRejected:
		return to == StatusClosed
	case StatusFailed:
		return to == StatusPendingInvestigation || to == StatusClosed
	default:
		return false
	}
}
