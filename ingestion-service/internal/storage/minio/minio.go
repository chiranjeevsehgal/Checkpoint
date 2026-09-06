// Package minio implements storage.ObjectStorage on MinIO / S3.
package minio

import (
	"context"
	"time"

	minioapi "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/storage"
)

// defaultRegion pins the signature region so presigning never needs a
// bucket-location lookup (MinIO's default region).
const defaultRegion = "us-east-1"

// Storage is the MinIO-backed ObjectStorage implementation.
type Storage struct {
	client *minioapi.Client
	public *minioapi.Client
}

// New connects to MinIO and ensures the bucket exists so later calls can
// assume it. Bucket creation is idempotent and safe across replicas.
//
// endpoint is the in-network address used for Stat/Delete/bucket calls,
// while publicEndpoint is the client-facing address baked into presigned
// URLs. They differ when the API runs inside Docker but clients upload
// from outside. Pass "" for publicEndpoint to reuse endpoint.
func New(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool, publicEndpoint string) (*Storage, error) {
	client, err := minioapi.New(endpoint, &minioapi.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: defaultRegion,
	})
	if err != nil {
		return nil, err
	}
	if publicEndpoint == "" {
		publicEndpoint = endpoint
	}
	public, err := minioapi.New(publicEndpoint, &minioapi.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: defaultRegion,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minioapi.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return &Storage{client: client, public: public}, nil
}

// CreateUploadURL mints a presigned PUT URL for direct client upload.
// The URL points at the public endpoint; the API never sees the bytes.
func (s *Storage) CreateUploadURL(
	ctx context.Context,
	bucket string,
	objectKey string,
	_ string,
	expiry time.Duration,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url, err := s.public.PresignedPutObject(ctx, bucket, objectKey, expiry)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// StatObject verifies the object exists and reports its size for the
// /complete validation step.
func (s *Storage) StatObject(
	ctx context.Context,
	bucket string,
	objectKey string,
) (storage.ObjectInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	info, err := s.client.StatObject(ctx, bucket, objectKey, minioapi.StatObjectOptions{})
	if err != nil {
		if minioapi.ToErrorResponse(err).Code == "NoSuchKey" {
			return storage.ObjectInfo{}, storage.ErrObjectNotFound
		}
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

// DeleteObject removes an object, used for abandoned or rejected uploads.
func (s *Storage) DeleteObject(
	ctx context.Context,
	bucket string,
	objectKey string,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.client.RemoveObject(ctx, bucket, objectKey, minioapi.RemoveObjectOptions{})
}

// Ping checks MinIO reachability for the readiness probe.
func (s *Storage) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.client.ListBuckets(ctx)
	return err
}
