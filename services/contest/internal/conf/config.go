package conf

import (
	"fmt"
	"strings"
	"time"
)

func ValidateConfig(c *Bootstrap) error {
	if c == nil || c.GetService() == nil || strings.TrimSpace(c.GetService().GetName()) == "" {
		return fmt.Errorf("contest service name is required")
	}
	if c.GetServer() == nil || c.GetServer().GetGrpc() == nil || strings.TrimSpace(c.GetServer().GetGrpc().GetAddress()) == "" {
		return fmt.Errorf("contest gRPC address is required")
	}
	if c.GetData() == nil || strings.TrimSpace(c.GetData().GetMysqlDsn()) == "" {
		return fmt.Errorf("contest mysql dsn is required")
	}
	if m := c.GetMessaging(); m == nil || m.GetUrl() == "" || m.GetExchange() == "" || m.GetQueue() == "" || m.GetDeadLetterQueue() == "" || m.GetPrefetch() <= 0 || m.GetPrefetch() > 256 {
		return fmt.Errorf("invalid contest messaging configuration")
	}
	if auth := c.GetInternalAuth(); auth != nil && strings.TrimSpace(auth.GetPublicKeyFile()) != "" {
		if _, err := time.ParseDuration(auth.GetMaxTokenTtl()); err != nil {
			return fmt.Errorf("invalid internal auth max token ttl: %w", err)
		}
		if _, err := time.ParseDuration(auth.GetClockSkew()); err != nil {
			return fmt.Errorf("invalid internal auth clock skew: %w", err)
		}
	}
	return nil
}
