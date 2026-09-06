// Package queue is the Kafka boundary for transcription jobs. The outbox
// dispatcher depends only on Publisher and never on a Kafka client.
package queue

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// PublishTimeout bounds a single sync produce. The dispatcher owns retry
// and backoff; a slow broker must not hold the outbox lease.
const PublishTimeout = 5 * time.Second

// Message is one Kafka record to publish.
type Message struct {
	Key     string
	Headers map[string]string
	Value   []byte
}

// Publisher publishes one message. Implementations must be safe for
// sequential use by the dispatcher and idempotent on retry.
type Publisher interface {
	Publish(ctx context.Context, msg Message) error
	Close()
}

// FranzProducer publishes to a fixed topic with acks=all and key-based
// partitioning. Delivery is at-least-once: the dispatcher may retry after
// a successful publish if MarkDelivered fails, so consumers must dedup on
// the event_id header.
type FranzProducer struct {
	client *kgo.Client
	topic  string
}

// NewFranzProducer builds a producer for topic. brokers is a
// comma-separated list like "kafka:9092". clientID identifies this replica
// in broker logs. The client connects lazily; unreachable brokers surface
// as Publish errors for the dispatcher to retry, not as constructor errors.
func NewFranzProducer(brokers, topic, clientID string) (*FranzProducer, error) {
	seeds := splitBrokers(brokers)
	if len(seeds) == 0 {
		return nil, errors.New("kafka brokers must not be empty")
	}
	if strings.TrimSpace(topic) == "" {
		return nil, errors.New("kafka topic must not be empty")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(seeds...),
		kgo.DefaultProduceTopic(topic),
		kgo.ClientID(clientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		// Idempotent writes are on by default when acks=all; retries
		// keep at-least-once delivery safe for dispatcher retries.
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
		kgo.ProduceRequestTimeout(PublishTimeout),
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &FranzProducer{client: client, topic: topic}, nil
}

func splitBrokers(brokers string) []string {
	var seeds []string
	for _, s := range strings.Split(brokers, ",") {
		if s = strings.TrimSpace(s); s != "" {
			seeds = append(seeds, s)
		}
	}
	return seeds
}

// Publish produces one record and waits for broker acknowledgement.
// A nil error means Kafka durably accepted it; anything else must be
// retried by the caller.
func (p *FranzProducer) Publish(ctx context.Context, msg Message) error {
	ctx, cancel := context.WithTimeout(ctx, PublishTimeout)
	defer cancel()

	headers := make([]kgo.RecordHeader, 0, len(msg.Headers))
	for k, v := range msg.Headers {
		headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	rec := &kgo.Record{
		Topic:   p.topic,
		Key:     []byte(msg.Key),
		Headers: headers,
		Value:   msg.Value,
	}
	results := p.client.ProduceSync(ctx, rec)
	return results.FirstErr()
}

// Close flushes and shuts down the client.
func (p *FranzProducer) Close() {
	if p != nil && p.client != nil {
		p.client.Close()
	}
}
