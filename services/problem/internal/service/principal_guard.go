package service

import (
	"context"
	"fmt"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
)

func requestContextFromPrincipal(ctx context.Context) (*commonv1.RequestContext, error) {
	p, ok := internalauth.PrincipalFromContext(ctx)
	if !ok || p.ActorID <= 0 {
		return nil, fmt.Errorf("trusted principal is missing")
	}
	return &commonv1.RequestContext{UserId: p.ActorID, Roles: append([]string(nil), p.ActorRoles...), RequestId: p.RequestID, TraceId: p.TraceID}, nil
}

// verifyRequestContext is a compatibility guard until RequestContext is
// removed from the Problem protobuf. When the internal middleware is active,
// the signed JWT is the authority and a mismatching body context is rejected.
func verifyRequestContext(ctx context.Context, request *commonv1.RequestContext) error {
	p, ok := internalauth.PrincipalFromContext(ctx)
	if !ok {
		return nil
	}
	if request == nil || !p.MatchesRequestContext(request.GetUserId(), request.GetRoles()) {
		return fmt.Errorf("signed actor does not match request context")
	}
	return nil
}
