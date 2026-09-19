package data

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const dataTestRevision = "01K5C6Y7N8P9Q0R1S2T3V4W5X6"

type recordingPutter struct {
	bucket, key string
	body        []byte
	size        int64
	options     minio.PutObjectOptions
	err         error
}

func (p *recordingPutter) PutObject(_ context.Context, bucket, key string, reader io.Reader, size int64, options minio.PutObjectOptions) (minio.UploadInfo, error) {
	p.bucket, p.key, p.size, p.options = bucket, key, size, options
	p.body, _ = io.ReadAll(reader)
	return minio.UploadInfo{}, p.err
}

func TestMinIOSourceStorePut(t *testing.T) {
	putter := &recordingPutter{}
	store := &MinIOSourceStore{client: putter, bucket: "submission-source", newID: func() string { return dataTestRevision }}
	source := []byte("package main\n")
	object, err := store.Put(context.Background(), "go", source)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if object.Key != "sources/"+dataTestRevision+"/source.go" || putter.key != object.Key || putter.bucket != "submission-source" {
		t.Fatalf("object = %+v, upload key = %q bucket = %q", object, putter.key, putter.bucket)
	}
	if object.SHA256 != "df1d036cbbf3df46e2045071e082245ece204c7f53ecf0a4e022bff9bb228f47" || object.Size != int64(len(source)) {
		t.Fatalf("object metadata = %+v", object)
	}
	if string(putter.body) != string(source) || putter.size != int64(len(source)) {
		t.Fatalf("uploaded body = %q size = %d", putter.body, putter.size)
	}
	if got := putter.options.Header().Get("If-None-Match"); got != "*" {
		t.Fatalf("If-None-Match = %q, want *", got)
	}
}

func TestMinIOSourceStoreRejectsInvalidInputAndMapsFailure(t *testing.T) {
	store := &MinIOSourceStore{client: &recordingPutter{}, bucket: "submission-source", newID: func() string { return dataTestRevision }}
	if _, err := store.Put(context.Background(), "rust", []byte("x")); !biz.HasReason(err, biz.ReasonInvalidArgument) {
		t.Fatalf("unsupported language error = %v", err)
	}
	if _, err := store.Put(context.Background(), "go", []byte{}); !biz.HasReason(err, biz.ReasonInvalidArgument) {
		t.Fatalf("empty source error = %v", err)
	}
	store.client = &recordingPutter{err: errors.New("minio unavailable")}
	if _, err := store.Put(context.Background(), "go", []byte("x")); !biz.HasReason(err, biz.ReasonDependencyUnavailable) {
		t.Fatalf("storage failure error = %v", err)
	}
	store.newID = func() string { return strings.Repeat("x", biz.MaxObjectKeyLength) }
	if _, err := store.Put(context.Background(), "go", []byte("x")); !biz.HasReason(err, biz.ReasonInternal) {
		t.Fatalf("oversized key error = %v", err)
	}
}
