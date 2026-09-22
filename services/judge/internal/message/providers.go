package message

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

var ProviderSet = wire.NewSet(
	NewRabbitPublisher,
	wire.Bind(new(biz.MessagePublisher), new(*RabbitPublisher)),
)
