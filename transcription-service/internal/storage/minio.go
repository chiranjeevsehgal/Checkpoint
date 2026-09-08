package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"transcription-service/internal/config"
)

type MinIOClient struct {
	client *minio.Client
}

func NewMinIOClient(cfg config.MinIOConfig) (*MinIOClient, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.ResolvedAccessKey(), cfg.ResolvedSecretKey(), ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}
	return &MinIOClient{client: client}, nil
}

// FetchObject downloads the object fully into memory. Fine for short voice clips
func (m *MinIOClient) FetchObject(ctx context.Context, bucket, objectKey string) ([]byte, string, error) {
	obj, err := m.client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("getting object %s/%s: %w", bucket, objectKey, err)
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat object %s/%s: %w", bucket, objectKey, err)
	}

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, obj); err != nil {
		return nil, "", fmt.Errorf("reading object %s/%s: %w", bucket, objectKey, err)
	}

	return buf.Bytes(), info.ContentType, nil
}