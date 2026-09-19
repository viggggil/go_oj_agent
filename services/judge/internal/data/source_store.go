package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/oklog/ulid/v2"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type objectPutter interface {
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

type MinIOSourceStore struct {
	client objectPutter
	bucket string
	newID  func() string
}

func NewSourceStore(config *conf.Bootstrap) (*MinIOSourceStore, error) {
	if config == nil || config.GetStorage() == nil || config.GetStorage().GetMinio() == nil {
		return nil, fmt.Errorf("judge minio config is required")
	}
	cfg := config.GetStorage().GetMinio()
	if strings.TrimSpace(cfg.GetEndpoint()) == "" || cfg.GetAccessKey() == "" || cfg.GetSecretKey() == "" || strings.TrimSpace(cfg.GetSourceBucket()) == "" {
		return nil, fmt.Errorf("judge minio endpoint, credentials and source bucket are required")
	}
	client, err := minio.New(cfg.GetEndpoint(), &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.GetAccessKey(), cfg.GetSecretKey(), ""),
		Secure: cfg.GetUseSsl(),
	})
	if err != nil {
		return nil, err
	}
	return &MinIOSourceStore{client: client, bucket: cfg.GetSourceBucket(), newID: func() string { return ulid.Make().String() }}, nil
}

func (s *MinIOSourceStore) Put(ctx context.Context, language string, source []byte) (biz.SourceObject, error) {
	if s == nil || s.client == nil || strings.TrimSpace(s.bucket) == "" || s.newID == nil {
		return biz.SourceObject{}, biz.ErrorInternal("source store is not configured")
	}
	extension, ok := biz.LanguageExtension(language)
	if !ok {
		return biz.SourceObject{}, biz.ErrorInvalidArgument("unsupported submission language")
	}
	if len(source) == 0 || int64(len(source)) > biz.MaxSourceSizeBytes {
		return biz.SourceObject{}, biz.ErrorInvalidArgument("source size must be between 1 and %d bytes", biz.MaxSourceSizeBytes)
	}
	sourceID := s.newID()
	if _, err := ulid.ParseStrict(sourceID); err != nil {
		return biz.SourceObject{}, biz.ErrorInternal("generated source object id is invalid")
	}
	key := fmt.Sprintf("sources/%s/source.%s", sourceID, extension)
	if len(key) > biz.MaxObjectKeyLength {
		return biz.SourceObject{}, biz.ErrorInternal("generated source object key is too long")
	}
	digest := sha256.Sum256(source)
	options := minio.PutObjectOptions{ContentType: "text/plain; charset=utf-8"}
	options.SetMatchETagExcept("*")
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(source), int64(len(source)), options); err != nil {
		return biz.SourceObject{}, biz.ErrorDependencyUnavailable("source storage is unavailable")
	}
	return biz.SourceObject{Key: key, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(source))}, nil
}

var _ biz.SourceStore = (*MinIOSourceStore)(nil)
