package sandbox

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
)

var ProviderSet = wire.NewSet(NewGoJudgeFromConfig, wire.Bind(new(Executor), new(*GoJudge)))

func NewGoJudgeFromConfig(config *conf.Bootstrap) (*GoJudge, func(), error) {
	timeout, err := conf.ParseDuration(config.Sandbox.GoJudge.Timeout, defaultRPCTimeout)
	if err != nil {
		return nil, func() {}, err
	}
	return NewGoJudge(config.Sandbox.GoJudge.Endpoint, config.Sandbox.GoJudge.Token, timeout)
}
