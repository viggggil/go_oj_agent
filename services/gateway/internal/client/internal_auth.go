package client

import "github.com/viggggil/go_oj_agent/services/gateway/internal/conf"

func internalAudience(client *conf.ClientProto, fallback string) string {
	if client != nil && client.GetName() != "" {
		return client.GetName()
	}
	return fallback
}
