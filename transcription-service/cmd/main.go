package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"transcription-service/internal/config"
	"transcription-service/internal/kafka"
	"transcription-service/internal/model"
	"transcription-service/internal/provider"
	"transcription-service/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	transcriber, err := buildProvider(cfg)
	if err != nil {
		log.Fatalf("building provider: %v", err)
	}
	log.Printf("using transcription provider: %s", transcriber.Name())

	minioClient, err := storage.NewMinIOClient(cfg.MinIO)
	if err != nil {
		log.Fatalf("connecting to minio: %v", err)
	}

	pgStore, err := storage.NewPostgresStore(ctx, cfg.Postgres.DSN())
	if err != nil {
		log.Fatalf("connecting to postgres: %v", err)
	}
	defer pgStore.Close()

	consumer := kafka.NewConsumer(cfg.Kafka)
	defer consumer.Close()

	producer := kafka.NewProducer(cfg.Kafka)
	defer producer.Close()

	log.Printf("listening on kafka topic %q, publishing to %q and %q",
		cfg.Kafka.ConsumeTopic, cfg.Kafka.ProduceTopicEmbedding, cfg.Kafka.ProduceTopicExtraction)

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return
		default:
		}

		var event model.TranscriptionRequestedEvent
		msg, err := consumer.ReadMessage(ctx, &event)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("read error: %v", err)
			continue
		}

		if err := event.Validate(); err != nil {
			// Malformed identifiers never succeed on redelivery, so drop the
			// message instead of looping on it.
			log.Printf("dropping invalid event: %v", err)
			if cerr := consumer.Commit(ctx, msg); cerr != nil {
				log.Printf("commit error for invalid event: %v", cerr)
			}
			continue
		}

		if err := handleMessage(ctx, event, transcriber, minioClient, pgStore, producer, cfg.Kafka); err != nil {
			// Not committing here means this message will be
			// redelivered on restart — intentional, so a failed
			// transcription isn't silently lost.
			log.Printf("failed to process audio_id=%s: %v", event.Data.AudioID, err)
			continue
		}

		if err := consumer.Commit(ctx, msg); err != nil {
			log.Printf("commit error for audio_id=%s: %v", event.Data.AudioID, err)
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
	log.Printf("processing audio_id=%s bucket=%s key=%s", data.AudioID, data.Bucket, data.ObjectKey)

	audio, fetchedContentType, err := minioClient.FetchObject(ctx, data.Bucket, data.ObjectKey)
	if err != nil {
		return err
	}
	contentType := data.ContentType
	if contentType == "" {
		contentType = fetchedContentType
	}

	result, err := transcriber.Transcribe(ctx, audio, contentType)
	if err != nil {
		return err
	}
	result.AudioID = data.AudioID
	result.UserID = data.UserID

	if err := pgStore.SaveTranscript(ctx, result); err != nil {
		return err
	}

	if err := publishEmbeddingJob(ctx, producer, kafkaCfg.ProduceTopicEmbedding, result); err != nil {
		return err
	}
	if err := publishExtractionJob(ctx, producer, kafkaCfg.ProduceTopicExtraction, result); err != nil {
		return err
	}

	log.Printf("completed audio_id=%s duration=%.2fs", data.AudioID, result.DurationSeconds)
	return nil
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
		return nil, unsupportedProviderErr(cfg.Provider)
	}
}

func unsupportedProviderErr(name string) error {
	return &unsupportedProviderError{name: name}
}

type unsupportedProviderError struct{ name string }

func (e *unsupportedProviderError) Error() string {
	return "unsupported transcription provider: " + e.name
}