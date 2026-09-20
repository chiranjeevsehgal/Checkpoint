package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	kafkago "github.com/segmentio/kafka-go"

	"transcription-service/internal/config"
)

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
// Returns the raw kafka message so the caller can commit it after
// successful processing.
func (c *Consumer) ReadMessage(ctx context.Context, out interface{}) (kafkago.Message, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		return kafkago.Message{}, fmt.Errorf("fetching message: %w", err)
	}
	if err := json.Unmarshal(msg.Value, out); err != nil {
		return msg, fmt.Errorf("unmarshalling message: %w", err)
	}
	return msg, nil
}

// Commit marks a message as processed. Only call this after the downstream work (transcription + storage + publish) has succeeded, `so a crash mid-processing means the message gets redelivered.
func (c *Consumer) Commit(ctx context.Context, msg kafkago.Message) error {
	return c.reader.CommitMessages(ctx, msg)
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}

type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(cfg config.KafkaConfig) *Producer {
	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(cfg.Brokers...),
		Balancer:     &kafkago.LeastBytes{},
		RequiredAcks: kafkago.RequireAll,
		// Topic intentionally left unset on the writer itself — this service publishes to two different topics (embedding + extraction jobs), so each message specifies its own topic.
	}
	return &Producer{writer: writer}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, value interface{}) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshalling message: %w", err)
	}
	err = p.writer.WriteMessages(ctx, kafkago.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	})
	if err != nil {
		return fmt.Errorf("publishing message to %s: %w", topic, err)
	}
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}