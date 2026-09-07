package auth

// AccessTokenClaims 是 user-service 签发、Gateway 验证的访问令牌载荷。
type AccessTokenClaims struct {
	Subject   int64    `json:"sub"`
	Username  string   `json:"username"`
	Roles     []string `json:"roles"`
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	TokenID   string   `json:"jti"`
}
