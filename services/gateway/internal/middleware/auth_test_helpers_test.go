package middleware

import "github.com/viggggil/go_oj_agent/services/gateway/internal/conf"

func testAuthConfig() *conf.Bootstrap {
	return &conf.Bootstrap{
		Auth: &conf.AuthProto{
			AccessTokenKey: "test-secret",
			Issuer:         "go-oj-agent",
			Audience:       "go-oj-gateway",
		},
	}
}
