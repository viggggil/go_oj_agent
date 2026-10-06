package main

import (
	"fmt"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

// Kratos 默认把环境占位符保留为字符串。单独解析开关，避免改变已有凭据/端点的解析类型。
func applyAgentEnableOverride(source config.Config, target *conf.Bootstrap) error {
	value := source.Value("CLIENTS_AGENT_ENABLED")
	if value.Load() == nil {
		return nil
	}
	enabled, err := value.Bool()
	if err != nil {
		return fmt.Errorf("invalid Agent enabled configuration")
	}
	if target.Clients == nil {
		target.Clients = &conf.ClientsProto{}
	}
	if target.Clients.Agent == nil {
		target.Clients.Agent = &conf.AgentProto{}
	}
	target.Clients.Agent.Enabled = enabled
	return nil
}
