package model

import "testing"

func TestDaySourcesIsEmpty(t *testing.T) {
	if !(DaySources{}).IsEmpty() {
		t.Fatal("no transcripts must be empty")
	}
	if (DaySources{Transcripts: []string{"x"}}).IsEmpty() {
		t.Fatal("a transcript must not be empty")
	}
	if !(DaySources{Todos: []string{"x"}}).IsEmpty() {
		t.Fatal("items without a transcript are not a source day")
	}
}
