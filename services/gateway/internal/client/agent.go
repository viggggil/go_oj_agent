package client

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

const AgentChatPath = "/api/v1/agent/chat"
const AgentChatOperation = "HTTP POST " + AgentChatPath

type AgentLimits struct {
	ConnectTimeout, HeaderTimeout, MaxDuration, IdleTimeout, WriteTimeout time.Duration
	MaxConcurrent, MaxRequestBytes, MaxFrameBytes                         int
}

func DefaultAgentLimits() AgentLimits {
	return AgentLimits{3 * time.Second, 5 * time.Second, 120 * time.Second, 20 * time.Second, 5 * time.Second, 8, 262144, 262144}
}

type AgentClient struct {
	endpoint string
	baseURL  string
	signer   *internalauth.Signer
	http     *http.Client
	limits   AgentLimits
	slots    chan struct{}
}

func NewAgentClient(config *conf.Bootstrap) (*AgentClient, func(), error) {
	limits := DefaultAgentLimits()
	var settings *conf.AgentProto
	if config != nil && config.GetClients() != nil {
		settings = config.GetClients().GetAgent()
	}
	if settings == nil || !settings.GetEnabled() {
		return &AgentClient{limits: limits}, func() {}, nil
	}
	for _, entry := range []struct {
		value   string
		target  *time.Duration
		maximum time.Duration
	}{
		{settings.GetConnectTimeout(), &limits.ConnectTimeout, 30 * time.Second},
		{settings.GetResponseHeaderTimeout(), &limits.HeaderTimeout, 30 * time.Second},
		{settings.GetMaxDuration(), &limits.MaxDuration, 360 * time.Second},
		{settings.GetIdleTimeout(), &limits.IdleTimeout, 60 * time.Second},
		{settings.GetWriteTimeout(), &limits.WriteTimeout, 30 * time.Second},
	} {
		if entry.value != "" {
			duration, err := time.ParseDuration(entry.value)
			if err != nil || duration <= 0 || duration > entry.maximum {
				return nil, nil, fmt.Errorf("invalid Agent timeout configuration")
			}
			*entry.target = duration
		}
	}
	for _, entry := range []struct {
		value            uint32
		target           *int
		minimum, maximum uint32
	}{
		{settings.GetMaxConcurrent(), &limits.MaxConcurrent, 1, 128},
		{settings.GetMaxRequestBytes(), &limits.MaxRequestBytes, 1024, 1048576},
		{settings.GetMaxFrameBytes(), &limits.MaxFrameBytes, 1024, 1048576},
	} {
		if entry.value != 0 {
			if entry.value < entry.minimum || entry.value > entry.maximum {
				return nil, nil, fmt.Errorf("invalid Agent size/concurrency configuration")
			}
			*entry.target = int(entry.value)
		}
	}
	endpoint, err := url.Parse(settings.GetEndpoint())
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, nil, fmt.Errorf("invalid Agent HTTP endpoint")
	}
	auth := config.GetAuth()
	if auth == nil || auth.GetInternalPrivateKeyFile() == "" || auth.GetInternalIssuer() != "go-oj-gateway" {
		return nil, nil, fmt.Errorf("Agent requires Gateway internal signing configuration")
	}
	key, err := os.ReadFile(auth.GetInternalPrivateKeyFile())
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read Gateway signing key")
	}
	block, _ := pem.Decode(key)
	if block == nil {
		return nil, nil, fmt.Errorf("invalid Gateway signing configuration")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	private, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok || private.N.BitLen() < 2048 || len(auth.GetInternalKeyId()) > 128 {
		return nil, nil, fmt.Errorf("invalid Gateway signing configuration")
	}
	ttl := 30 * time.Second
	if auth.GetInternalTokenTtl() != "" {
		ttl, err = time.ParseDuration(auth.GetInternalTokenTtl())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid Gateway delegation TTL")
		}
	}
	if ttl < time.Second || ttl > time.Minute {
		return nil, nil, fmt.Errorf("invalid Gateway delegation TTL")
	}
	signer, err := internalauth.NewSigner(key, auth.GetInternalKeyId(), "go-oj-gateway", "agent-service", "gateway-service", ttl, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Gateway signing configuration")
	}
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: limits.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: limits.ConnectTimeout, ResponseHeaderTimeout: limits.HeaderTimeout,
		MaxConnsPerHost: limits.MaxConcurrent, MaxIdleConnsPerHost: limits.MaxConcurrent,
		IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 16384,
		DisableCompression: true,
	}
	result := &AgentClient{
		baseURL:  strings.TrimSuffix(endpoint.String(), "/"),
		endpoint: strings.TrimSuffix(endpoint.String(), "/") + AgentChatPath, signer: signer,
		http:   &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		limits: limits, slots: make(chan struct{}, limits.MaxConcurrent),
	}
	return result, transport.CloseIdleConnections, nil
}

// JSON 固定内部目标和公开路径白名单；不重试、不转发调用方身份 Header。
func (c *AgentClient) JSON(ctx context.Context, method, path, query string, body []byte) (*http.Response, error) {
	operation, err := AgentOperation(method, path)
	if err != nil || !c.Enabled() || ValidateAgentContext(ctx) != nil {
		return nil, fmt.Errorf("invalid Agent request")
	}
	rc, _ := gatewaymw.RequestContextFromContext(ctx)
	token, err := c.signer.Sign(internalauth.Claims{ActorID: rc.GetUserId(), ActorRoles: rc.GetRoles(), RequestID: rc.GetRequestId(), RPC: operation, TokenID: uuid.NewString()})
	if err != nil {
		return nil, err
	}
	target := c.baseURL + path
	if query != "" {
		target += "?" + query
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(request)
}

func (c *AgentClient) Limits() AgentLimits {
	if c == nil {
		return DefaultAgentLimits()
	}
	return c.limits
}

func (c *AgentClient) Enabled() bool { return c != nil && c.http != nil && c.signer != nil }

func (c *AgentClient) Acquire() bool {
	if !c.Enabled() {
		return false
	}
	select {
	case c.slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (c *AgentClient) Release() { <-c.slots }

// ValidateAgentContext 与 Python 委托模型保持相同边界，建流前拒绝异常身份字段。
func ValidateAgentContext(ctx context.Context) error {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok || rc.GetUserId() <= 0 || rc.GetRequestId() == "" || !utf8.ValidString(rc.GetRequestId()) || utf8.RuneCountInString(rc.GetRequestId()) > 128 || len(rc.GetRoles()) > 32 {
		return fmt.Errorf("invalid Agent request context")
	}
	for _, role := range rc.GetRoles() {
		if role == "" || !utf8.ValidString(role) || utf8.RuneCountInString(role) > 64 {
			return fmt.Errorf("invalid Agent request context")
		}
	}
	return nil
}

// Open 不重试 POST、不跟随重定向，身份由已认证上下文生成。
func (c *AgentClient) Open(ctx context.Context, body []byte) (*http.Response, error) {
	if !c.Enabled() || ValidateAgentContext(ctx) != nil {
		return nil, fmt.Errorf("invalid Agent request context")
	}
	rc, _ := gatewaymw.RequestContextFromContext(ctx)
	token, err := c.signer.Sign(internalauth.Claims{ActorID: rc.GetUserId(), ActorRoles: rc.GetRoles(), RequestID: rc.GetRequestId(), RPC: AgentChatOperation, TokenID: uuid.NewString()})
	if err != nil {
		return nil, fmt.Errorf("Agent delegation failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	// http.Transport 仅对带 GetBody 的幂等请求执行重试；不附加幂等标记。
	request.GetBody = nil
	return c.http.Do(request)
}
