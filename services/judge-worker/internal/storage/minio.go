package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
)

type Reader interface {
	Get(context.Context, string, string) ([]byte, error)
}

const maxObjectSize = 64 << 20

type minioGetter interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
}

type MinIOReader struct{ client minioGetter }

func NewMinIOReader(config *conf.Bootstrap) (*MinIOReader, error) {
	if config == nil || strings.TrimSpace(config.Storage.MinIO.Endpoint) == "" || config.Storage.MinIO.AccessKey == "" || config.Storage.MinIO.SecretKey == "" {
		return nil, fmt.Errorf("judge-worker minio endpoint and credentials are required")
	}
	client, err := minio.New(config.Storage.MinIO.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(config.Storage.MinIO.AccessKey, config.Storage.MinIO.SecretKey, ""), Secure: config.Storage.MinIO.UseSSL})
	if err != nil {
		return nil, err
	}
	return &MinIOReader{client: client}, nil
}

func (r *MinIOReader) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	if r == nil || r.client == nil {
		return nil, biz.NewSystemError("JUDGE_INPUT_STORE_UNAVAILABLE", true, fmt.Errorf("reader is not configured"))
	}
	object, err := r.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, biz.NewSystemError("JUDGE_INPUT_STORE_UNAVAILABLE", true, err)
	}
	defer object.Close()
	content, err := io.ReadAll(io.LimitReader(object, maxObjectSize+1))
	if err != nil {
		return nil, biz.NewSystemError("JUDGE_INPUT_STORE_UNAVAILABLE", true, err)
	}
	if len(content) > maxObjectSize {
		return nil, biz.NewSystemError("JUDGE_INPUT_TOO_LARGE", false, fmt.Errorf("object exceeds %d bytes", maxObjectSize))
	}
	return content, nil
}
