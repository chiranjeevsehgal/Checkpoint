package main

import (
	"errors"
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

func TestBuildRetryHandoffEnvelope(t *testing.T) {
	original := []byte(`{"schema_version":2,"event_id":"e1","data":{"user_id":"u1"}}`)
	evt := buildRetryHandoff("extraction.jobs.v1", "llm_call", "PROVIDER_ERROR", "boom", original)

	if evt.SchemaVersion != model.SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", evt.SchemaVersion, model.SchemaVersion)
	}
	if evt.EventType != model.EventTypeRetryRequested {
		t.Fatalf("event_type = %q, want %q", evt.EventType, model.EventTypeRetryRequested)
	}
	if evt.EventID == "" {
		t.Fatal("event_id must be set")
	}
	if evt.Data.SourceService != "extraction-service" {
		t.Fatalf("source_service = %q", evt.Data.SourceService)
	}
	if evt.Data.SourceTopic != "extraction.jobs.v1" || evt.Data.Stage != "llm_call" ||
		evt.Data.ErrorCode != "PROVIDER_ERROR" || evt.Data.ErrorMessage != "boom" {
		t.Fatalf("unexpected handoff data: %+v", evt.Data)
	}
	if string(evt.Data.OriginalEvent) != string(original) {
		t.Fatalf("original_event = %q, want %q", evt.Data.OriginalEvent, original)
	}
}

func TestStageAndCode(t *testing.T) {
	stage, code := stageAndCode(staged("parse", "PARSE_ERROR", errors.New("bad json")))
	if stage != "parse" || code != "PARSE_ERROR" {
		t.Fatalf("staged error lost its stage/code: %s/%s", stage, code)
	}
	stage, code = stageAndCode(errors.New("plain"))
	if stage != "unknown" || code != "PROCESSING_ERROR" {
		t.Fatalf("unstaged error fallback wrong: %s/%s", stage, code)
	}
}

func TestOriginalEventID(t *testing.T) {
	if got := originalEventID([]byte(`{"event_id":"evt-1"}`)); got != "evt-1" {
		t.Fatalf("event id = %q, want evt-1", got)
	}
	if got := originalEventID([]byte("not json")); got != "" {
		t.Fatalf("unparseable payload must yield empty key, got %q", got)
	}
}
