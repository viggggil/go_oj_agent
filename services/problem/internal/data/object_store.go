package data

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
)

type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIOStore(config *conf.Bootstrap) (*MinIOStore, error) {
	if config == nil || config.GetStorage() == nil || config.GetStorage().GetMinio() == nil {
		return nil, fmt.Errorf("minio config is required")
	}
	cfg := config.GetStorage().GetMinio()
	if cfg.GetEndpoint() == "" || cfg.GetAccessKey() == "" || cfg.GetSecretKey() == "" || cfg.GetBucket() == "" {
		return nil, fmt.Errorf("minio endpoint, credentials and bucket are required")
	}
	client, err := minio.New(cfg.GetEndpoint(), &minio.Options{Creds: credentials.NewStaticV4(cfg.GetAccessKey(), cfg.GetSecretKey(), ""), Secure: cfg.GetUseSsl()})
	if err != nil {
		return nil, err
	}
	return &MinIOStore{client: client, bucket: cfg.GetBucket()}, nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, content []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *MinIOStore) PutImmutable(ctx context.Context, key string, content []byte, contentType string) error {
	options := minio.PutObjectOptions{ContentType: contentType}
	options.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), options)
	return err
}

func (s *MinIOStore) Get(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return io.ReadAll(object)
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
