package model

import "time"

const (
	PeriodDaily  = "daily"
	PeriodWeekly = "weekly"
)

// UserRef is a candidate user plus their stored IANA zone. An empty Timezone
// means the caller should fall back to the configured default.
type UserRef struct {
	UserID   string
	Timezone string
}

// DaySources is one local day's raw material for a summary.
type DaySources struct {
	Transcripts []string
	Todos       []string
	Reminders   []string
	Insights    []string
}

// IsEmpty reports whether the day has nothing to summarize.
func (d DaySources) IsEmpty() bool {
	return len(d.Transcripts) == 0
}

// Summary is one stored narrative for a period.
type Summary struct {
	UserID      string
	Period      string
	PeriodStart time.Time
	Text        string
	Model       string
}
