package queue

import (
	"testing"
)

func TestNewFranzProducerValidation(t *testing.T) {
	if _, err := NewFranzProducer("", "transcription.jobs.v1", "ingestion-test"); err == nil {
		t.Fatal("empty brokers must fail")
	}
	if _, err := NewFranzProducer("  ,  ", "transcription.jobs.v1", "ingestion-test"); err == nil {
		t.Fatal("blank brokers must fail")
	}
	if _, err := NewFranzProducer("kafka:9092", "", "ingestion-test"); err == nil {
		t.Fatal("empty topic must fail")
	}
	if _, err := NewFranzProducer("kafka:9092", "  ", "ingestion-test"); err == nil {
		t.Fatal("blank topic must fail")
	}
}

func TestSplitBrokers(t *testing.T) {
	got := splitBrokers("kafka:9092, kafka2:9092 ,,")
	if len(got) != 2 || got[0] != "kafka:9092" || got[1] != "kafka2:9092" {
		t.Fatalf("unexpected split: %q", got)
	}
	if len(splitBrokers("")) != 0 {
		t.Fatal("empty input must yield no seeds")
	}
}
