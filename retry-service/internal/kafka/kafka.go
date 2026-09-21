package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/google/uuid"

	"retry-service/internal/config"
	"retry-service/internal/model"
)

// MalformedMessageError marks a message whose JSON could not be unmarshalled.
// The message is still returned so the caller can route it to the DLQ and
// commit — a poison payload never succeeds on redelivery.
type MalformedMessageError struct {
	Cause error
}

func (m *MalformedMessageError) Error() string { return "malformed message json: " + m.Cause.Error() }
func (m *MalformedMessageError) Unwrap() error { return m.Cause }

type Consumer struct {
	reader *kafkago.Reader
}

func NewConsumer(cfg config.KafkaConfig) *Consumer {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   cfg.ConsumeTopic,
		GroupID: cfg.ConsumerGroup,
	})
	return &Consumer{reader: reader}
}

// ReadMessage blocks until a message arrives, unmarshalling it into out.
// Returns the raw kafka message even on unmarshal failure (as a
// *MalformedMessageError) so the caller can DLQ it.
func (c *Consumer) ReadMessage(ctx context.Context, out interface{}) (kafkago.Message, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		return kafkago.Message{}, fmt.Errorf("fetching message: %w", err)
	}
	if err := json.Unmarshal(msg.Value, out); err != nil {
		return msg, &MalformedMessageError{Cause: err}
	}
	return msg, nil
}

// Commit marks a message as processed. Only call this after the downstream
// work has succeeded; not committing means redelivery on restart.
func (c *Consumer) Commit(ctx context.Context, msg kafkago.Message) error {
	return c.reader.CommitMessages(ctx, msg)
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}

// Producer re-delivers retry payloads to their original source topics and
// writes poison handoffs to the retry topic's DLQ.
type Producer struct {
	writer   *kafkago.Writer
	dlqTopic string
}

func NewProducer(cfg config.KafkaConfig) *Producer {
	return &Producer{
		writer: &kafkago.Writer{
			Addr:     kafkago.TCP(cfg.Brokers...),
			Balancer: &kafkago.LeastBytes{},
		},
		dlqTopic: cfg.DLQTopic,
	}
}

// Publish re-delivers a retry job's original payload to its source topic,
// byte-identical to what the failing service consumed, with the original
// message key preserved for traceability. Source-topic consumers are
// idempotent upserters keyed on event_id, so an at-least-once re-delivery
// can neither duplicate nor corrupt work.
func (p *Producer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	msg := kafkago.Message{
		Topic: topic,
		Value: payload,
	}
	if key != "" {
		msg.Key = []byte(key)
	}
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("publishing retry payload to %s: %w", topic, err)
	}
	return nil
}

// SendToDLQ publishes the standard failure envelope to the DLQ topic. The
// poison handoff rides along unmodified inside data.original_event.
func (p *Producer) SendToDLQ(ctx context.Context, sourceTopic, errorCode, errorMessage string, original json.RawMessage) error {
	event := model.RetryFailedEvent{
		Envelope: model.Envelope{
			SchemaVersion: model.SchemaVersion,
			EventID:       uuid.NewString(),
			EventType:     model.EventTypeRetryFailed,
			OccurredAt:    time.Now().UTC().Format(time.RFC3339),
		},
		Data: model.RetryFailedData{
			SourceTopic:   sourceTopic,
			ErrorCode:     errorCode,
			ErrorMessage:  errorMessage,
			OriginalEvent: original,
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshalling dlq event: %w", err)
	}
	if err := p.writer.WriteMessages(ctx, kafkago.Message{
		Topic: p.dlqTopic,
		Value: payload,
	}); err != nil {
		return fmt.Errorf("publishing dlq event to %s: %w", p.dlqTopic, err)
	}
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
