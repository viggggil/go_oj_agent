package data

import "github.com/google/wire"

// ProviderSet is reserved for MySQL, Redis, and MinIO implementations.
var ProviderSet = wire.NewSet()
