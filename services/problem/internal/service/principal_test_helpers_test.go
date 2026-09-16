package service

import (
	"context"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
)

func adminContext() context.Context {
	return internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"admin"}})
}
func userContext() context.Context {
	return internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"user"}})
}
