package conf

import (
	"fmt"
	"regexp"
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
	return ValidateLeaderboardCache(c.GetLeaderboardCache())
}

// ValidateLeaderboardCache also guards direct client construction in tests/tools.
func ValidateLeaderboardCache(cache *LeaderboardCacheProto) error {
	if cache != nil && cache.GetEnabled() {
		if len(cache.GetAddresses()) == 0 || cache.GetDb() < 0 || !regexp.MustCompile(`^[a-zA-Z0-9:_-]+$`).MatchString(cache.GetNamespace()) {
			return fmt.Errorf("invalid leaderboard Redis addresses, database or namespace")
		}
		for _, address := range cache.GetAddresses() {
			if strings.TrimSpace(address) == "" {
				return fmt.Errorf("empty leaderboard Redis address")
			}
		}
		timeout, err := time.ParseDuration(cache.GetTimeout())
		if err != nil || timeout <= 0 || timeout > 3*time.Second {
			return fmt.Errorf("leaderboard Redis timeout must be positive and <= 3s")
		}
		if len(cache.GetAddresses()) > 1 && cache.GetDb() != 0 {
			return fmt.Errorf("Redis Cluster requires db 0")
		}
	}
	return nil
}
