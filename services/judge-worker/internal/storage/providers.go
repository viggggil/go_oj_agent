package storage

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
)

var ProviderSet = wire.NewSet(NewMinIOReader, wire.Bind(new(Reader), new(*MinIOReader)), NewLoaderFromConfig, wire.Bind(new(biz.TaskLoader), new(*Loader)))

func NewLoaderFromConfig(reader Reader, config *conf.Bootstrap) (*Loader, error) {
	return NewLoader(reader, config.Storage.MinIO.SourceBucket, config.Storage.MinIO.ProblemBucket)
}
