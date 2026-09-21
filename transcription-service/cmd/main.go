package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/google/uuid"

	"transcription-service/internal/config"
	"transcription-service/internal/kafka"
	"transcription-service/internal/language"
	"transcription-service/internal/model"
	"transcription-service/internal/observability"
	"transcription-service/internal/provider"
	"transcription-service/internal/storage"
)

// errStaleDeletedUser means the event belongs to an account that is being
// deleted. The message is committed and dropped rather than retried.
var errStaleDeletedUser = errors.New("stale event for deleted user")

// errOmitTranscript means the transcript is blank or in a language the user
// did not select. The message is committed and dropped rather than retried.
var errOmitTranscript = errors.New("transcript omitted")

// stageError carries where in the pipeline a failure happened so the retry
// service's audit log records the failure point.
type stageError struct {
	stage string
	code  string
	err   error
}

func (e *stageError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func staged(stage, code string, err error) error {
	return &stageError{stage: stage, code: code, err: err}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.SetDefault(observability.New("transcription-service"))

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	transcriber, err := buildProvider(cfg)
	if err != nil {
		slog.Error("building provider", "error", err)
		os.Exit(1)
	}
	slog.Info("using transcription provider", "provider", transcriber.Name())

	minioClient, err := storage.NewMinIOClient(cfg.MinIO)
	if err != nil {
		slog.Error("connecting to minio", "error", err)
		os.Exit(1)
	}

	pgStore, err := storage.NewPostgresStore(ctx, cfg.Postgres.DSN())
	if err != nil {
		slog.Error("connecting to postgres", "error", err)
		os.Exit(1)
	}
	defer pgStore.Close()

	consumer := kafka.NewConsumer(cfg.Kafka)
	defer consumer.Close()

	producer := kafka.NewProducer(cfg.Kafka)
	defer producer.Close()

	slog.Info("listening on kafka topic",
		"consume_topic", cfg.Kafka.ConsumeTopic,
		"embedding_topic", cfg.Kafka.ProduceTopicEmbedding,
		"extraction_topic", cfg.Kafka.ProduceTopicExtraction)

	health := observability.NewHealthServer(":9081")
	health.Start(ctx)
	health.SetReady(true)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		default:
		}

		var event model.TranscriptionRequestedEvent
		msg, err := consumer.ReadMessage(ctx, &event)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("read error", "error", err)
			continue
		}

		if err := event.Validate(); err != nil {
			// Malformed identifiers never succeed on redelivery, so drop the
			// message instead of looping on it.
			slog.Warn("dropping invalid event", "error", err)
			health.Inc("transcription_processed_invalid_total")
			if cerr := consumer.Commit(ctx, msg); cerr != nil {
				slog.Error("commit error for invalid event", "error", cerr)
			}
			continue
		}

		if err := handleMessage(ctx, event, transcriber, minioClient, pgStore, producer, cfg.Kafka); err != nil {
			if errors.Is(err, errStaleDeletedUser) {
				slog.Info("stale_deleted_user_event", "audio_id", event.Data.AudioID)
				health.Inc("transcription_processed_stale_total")
				if cerr := consumer.Commit(ctx, msg); cerr != nil {
					slog.Error("commit error for stale event", "error", cerr)
				}
				continue
			}
			if errors.Is(err, errOmitTranscript) {
				slog.Info("omitted transcript", "audio_id", event.Data.AudioID, "reason", err)
				health.Inc("transcription_processed_omitted_total")
				if cerr := consumer.Commit(ctx, msg); cerr != nil {
					slog.Error("commit error for omitted transcript", "error", cerr)
				}
				continue
			}
			// Transient failure: hand the original event to the central
			// retry service instead of hot-looping redelivery at fetch
			// speed. The handoff owns re-delivery from here.
			slog.Error("failed to process", "audio_id", event.Data.AudioID, "error", err)
			health.Inc("transcription_processed_failed_total")
			if herr := handoffToRetry(ctx, producer, cfg.Kafka, event, msg, err); herr != nil {
				// The handoff itself is not durable yet — leave the
				// message uncommitted so redelivery retries both.
				slog.Error("retry handoff failed, will redeliver", "audio_id", event.Data.AudioID, "error", herr)
				continue
			}
			if cerr := consumer.Commit(ctx, msg); cerr != nil {
				slog.Error("commit error", "audio_id", event.Data.AudioID, "error", cerr)
			}
			continue
		}

		health.Inc("transcription_processed_ok_total")
		if err := consumer.Commit(ctx, msg); err != nil {
			slog.Error("commit error", "audio_id", event.Data.AudioID, "error", err)
		}
	}
}

func handleMessage(
	ctx context.Context,
	event model.TranscriptionRequestedEvent,
	transcriber provider.Transcriber,
	minioClient *storage.MinIOClient,
	pgStore *storage.PostgresStore,
	producer *kafka.Producer,
	kafkaCfg config.KafkaConfig,
) error {
	data := event.Data
	slog.Info("processing", "audio_id", data.AudioID, "bucket", data.Bucket, "object_key", data.ObjectKey)

	deleting, err := pgStore.IsUserDeleting(ctx, data.UserID)
	if err != nil {
		return staged("check_deletion", "DB_ERROR", err)
	}
	if deleting {
		return errStaleDeletedUser
	}

	languages, err := pgStore.AllowedLanguages(ctx, data.UserID)
	if err != nil {
		return staged("load_languages", "DB_ERROR", err)
	}

	audio, fetchedContentType, err := minioClient.FetchObject(ctx, data.Bucket, data.ObjectKey)
	if err != nil {
		return staged("fetch_audio", "STORAGE_ERROR", err)
	}
	contentType := data.ContentType
	if contentType == "" {
		contentType = fetchedContentType
	}

	result, err := transcriber.Transcribe(ctx, audio, contentType, languages)
	if err != nil {
		return staged("transcribe", "PROVIDER_ERROR", err)
	}
	if reason := language.OmitReason(result.Text, result.Language, languages); reason != "" {
		return fmt.Errorf("%w: %s", errOmitTranscript, reason)
	}
	result.AudioID = data.AudioID
	result.UserID = data.UserID
	result.RecordedAt = data.RecordedAt

	// Re-check immediately before persistence: the account may have been
	// deleted while the expensive transcription ran.
	deleting, err = pgStore.IsUserDeleting(ctx, data.UserID)
	if err != nil {
		return staged("recheck_deletion", "DB_ERROR", err)
	}
	if deleting {
		return errStaleDeletedUser
	}

	if err := pgStore.SaveTranscript(ctx, result); err != nil {
		return staged("persist_transcript", "DB_ERROR", err)
	}

	if err := publishEmbeddingJob(ctx, producer, kafkaCfg.ProduceTopicEmbedding, result); err != nil {
		return staged("publish_embedding", "PUBLISH_ERROR", err)
	}
	if err := publishExtractionJob(ctx, producer, kafkaCfg.ProduceTopicExtraction, result); err != nil {
		return staged("publish_extraction", "PUBLISH_ERROR", err)
	}

	slog.Info("completed", "audio_id", data.AudioID, "duration_seconds", result.DurationSeconds)
	return nil
}

// handoffToRetry publishes a RETRY_REQUESTED handoff for a failed message.
// Keyed by the original event id so every handoff for one original lands on
// the same retry partition, keeping the attempt history in order.
func handoffToRetry(ctx context.Context, producer *kafka.Producer, kafkaCfg config.KafkaConfig, event model.TranscriptionRequestedEvent, msg kafkago.Message, err error) error {
	stage, code := "unknown", "PROCESSING_ERROR"
	var se *stageError
	if errors.As(err, &se) {
		stage, code = se.stage, se.code
	}
	handoff := buildRetryHandoff(kafkaCfg.ConsumeTopic, stage, code, err.Error(), json.RawMessage(msg.Value))
	return producer.Publish(ctx, kafkaCfg.RetryTopic, event.EventID, handoff)
}

// buildRetryHandoff is the pure handoff envelope, kept producer-free so it can
// be unit-tested without a broker.
func buildRetryHandoff(sourceTopic, stage, code, message string, originalEvent json.RawMessage) model.RetryRequestedEvent {
	return model.RetryRequestedEvent{
		Envelope: newEnvelope(model.EventTypeRetryRequested),
		Data: model.RetryRequestedData{
			SourceService: "transcription-service",
			SourceTopic:   sourceTopic,
			Stage:         stage,
			ErrorCode:     code,
			ErrorMessage:  message,
			OriginalEvent: originalEvent,
		},
	}
}

func publishEmbeddingJob(ctx context.Context, producer *kafka.Producer, topic string, result *model.TranscriptResult) error {
	evt := model.EmbeddingJobRequestedEvent{
		Envelope: newEnvelope("EMBEDDING_REQUESTED"),
		Data: model.EmbeddingJobData{
			AudioID:  result.AudioID,
			UserID:   result.UserID,
			Text:     result.Text,
			Language: result.Language,
		},
	}
	return producer.Publish(ctx, topic, result.AudioID, evt)
}

func publishExtractionJob(ctx context.Context, producer *kafka.Producer, topic string, result *model.TranscriptResult) error {
	evt := model.ExtractionJobRequestedEvent{
		Envelope: newEnvelope("EXTRACTION_REQUESTED"),
		Data: model.ExtractionJobData{
			AudioID:         result.AudioID,
			UserID:          result.UserID,
			Text:            result.Text,
			Language:        result.Language,
			SpeakerSegments: result.SpeakerSegments,
			RecordedAt:      result.RecordedAt,
		},
	}
	return producer.Publish(ctx, topic, result.AudioID, evt)
}

func newEnvelope(eventType string) model.Envelope {
	return model.Envelope{
		SchemaVersion: 2,
		EventID:       uuid.NewString(),
		EventType:     eventType,
		OccurredAt:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

func buildProvider(cfg *config.Config) (provider.Transcriber, error) {
	switch cfg.Provider {
	case "elevenlabs":
		return provider.NewElevenLabsProvider(cfg.Providers.ElevenLabs), nil
	case "deepgram":
		return provider.NewDeepgramProvider(cfg.Providers.Deepgram), nil
	default:
		return nil, fmt.Errorf("unsupported transcription provider %q: must be elevenlabs or deepgram", cfg.Provider)
	}
}