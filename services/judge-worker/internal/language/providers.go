package language

import (
	"time"

	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

var ProviderSet = wire.NewSet(NewGoRunnerFromConfig, wire.Bind(new(biz.LanguageRunner), new(*GoRunner)))

func NewGoRunnerFromConfig(executor sandbox.Executor, config *conf.Bootstrap) (*GoRunner, error) {
	compileTimeout, err := conf.ParseDuration(config.Language.Go.CompileTimeLimit, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return NewGoRunner(executor, GoConfig{
		CompilerPath: config.Language.Go.CompilerPath, CompileTimeLimit: compileTimeout,
		CompileMemoryBytes: config.Language.Go.CompileMemoryBytes, ProcessLimit: config.Language.Go.ProcessLimit,
		OutputLimitBytes: config.Language.Go.OutputLimitBytes,
	}), nil
}
