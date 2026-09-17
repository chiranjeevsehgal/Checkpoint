package main

import (
	"testing"

	"extraction-service/internal/model"
)

func mkJobs(texts ...string) []model.Job {
	jobs := make([]model.Job, len(texts))
	for i, txt := range texts {
		jobs[i] = model.Job{ID: int64(i + 1), Text: txt}
	}
	return jobs
}

func TestSplitByCharsKeepsUnderCap(t *testing.T) {
	jobs := mkJobs("aaaaa", "bbbbb", "ccccc", "ddddd") // 5 chars each
	groups := splitByChars(jobs, 10)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}
	if len(groups[0]) != 2 || len(groups[1]) != 2 {
		t.Fatalf("expected 2+2 split, got %d+%d", len(groups[0]), len(groups[1]))
	}
}

func TestSplitByCharsSingleOversizedJobIsOwnGroup(t *testing.T) {
	jobs := mkJobs("1234567890", "small", "small")
	groups := splitByChars(jobs, 8)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if len(groups[0]) != 1 {
		t.Fatalf("oversized job must be alone, group 0 has %d", len(groups[0]))
	}
}

func TestSplitByCharsEmptyAndAllFit(t *testing.T) {
	if got := splitByChars(nil, 100); got != nil {
		t.Fatalf("expected nil for no jobs, got %v", got)
	}
	groups := splitByChars(mkJobs("a", "b"), 1000)
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Fatalf("expected single group, got %+v", groups)
	}
}
