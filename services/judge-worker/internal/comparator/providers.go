package comparator

import (
	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

var ProviderSet = wire.NewSet(NewText, wire.Bind(new(biz.Comparator), new(*Text)))
