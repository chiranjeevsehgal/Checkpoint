package model

import "time"

// Delivery kinds. Every reminder yields one advance and one due notification.
const (
	KindAdvance = "advance"
	KindDue     = "due"
)

// Candidate is a reminder whose notification fire time has arrived. Topic is
// the owner's ntfy capability; the scheduler publishes to it.
type Candidate struct {
	UserID       string
	AudioID      string
	ReminderText string
	Kind         string
	FireAt       time.Time
	Topic        string
}
