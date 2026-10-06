package main

import (
	"testing"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

func TestAgentEnabledEnvironmentUsesBooleanWithoutChangingOtherFields(t *testing.T) {
	for _, value := range []string{"true", "false", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("KRATOS_CLIENTS_AGENT_ENABLED", value)
			source := config.New(config.WithSource(env.NewSource("KRATOS")))
			defer source.Close()
			if err := source.Load(); err != nil {
				t.Fatal(err)
			}
			target := &conf.Bootstrap{}
			err := applyAgentEnableOverride(source, target)
			if value == "invalid" {
				if err == nil {
					t.Fatal("invalid switch accepted")
				}
				return
			}
			if err != nil || target.GetClients().GetAgent().GetEnabled() != (value == "true") {
				t.Fatalf("config=%v error=%v", target, err)
			}
		})
	}
}
