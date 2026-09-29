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
	compileTimeout, err := conf.ParseDuration(config.Language.Go.CompileTimeLimit, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return NewGoRunner(executor, GoConfig{
		CompilerPath: config.Language.Go.CompilerPath, CompileTimeLimit: compileTimeout,
		CompileMemoryBytes: config.Language.Go.CompileMemoryBytes, ProcessLimit: config.Language.Go.ProcessLimit,
		CompileOutputBytes: 64 << 20,
		OutputLimitBytes:   config.Language.Go.OutputLimitBytes,
	}), nil
}

// NewLanguageRunnersFromConfig keeps each toolchain isolated while sharing the
// same go-judge connection. Defaults match the judge-worker image toolchain.
func NewLanguageRunnersFromConfig(executor sandbox.Executor, config *conf.Bootstrap) (map[string]biz.LanguageRunner, error) {
	goRunner, err := NewGoRunnerFromConfig(executor, config)
	if err != nil {
		return nil, err
	}
	runners := map[string]biz.LanguageRunner{
		"go":     goRunner,
		"c":      NewCommandRunner(executor, "c", "/usr/bin/gcc", "main.c", "main", func(source string) []string { return []string{"-std=c17", "-O2", "-pipe", "-o", "main", source} }),
		"cpp":    NewCommandRunner(executor, "cpp", "/usr/bin/g++", "main.cpp", "main", func(source string) []string { return []string{"-std=c++17", "-O2", "-pipe", "-o", "main", source} }),
		"java":   NewCommandRunner(executor, "java", "/usr/bin/javac", "Main.java", "Main.class", func(source string) []string { return []string{"-encoding", "UTF-8", source} }),
		"python": NewCommandRunner(executor, "python", "/usr/bin/python3", "main.py", "main.py", func(source string) []string { return []string{"-m", "py_compile", source} }),
	}
	return runners, nil
}
