package internalauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Actor struct {
	ID                 int64
	Roles              []string
	RequestID, TraceID string
}
type ActorResolver func(context.Context) Actor

func UnaryClientInterceptor(signer *Signer, resolve ActorResolver) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if signer == nil {
			return status.Error(codes.Unauthenticated, "internal signer is not configured")
		}
		actor := Actor{}
		if resolve != nil {
			actor = resolve(ctx)
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return status.Error(codes.Internal, "internal token id unavailable")
		}
		token, err := signer.Sign(Claims{ActorID: actor.ID, ActorRoles: append([]string(nil), actor.Roles...), RPC: method, RequestID: actor.RequestID, TraceID: actor.TraceID, TokenID: hex.EncodeToString(id)})
		if err != nil {
			return status.Error(codes.Unauthenticated, "internal token unavailable")
		}
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return invoke(ctx, method, req, reply, cc, opts...)
	}
}

func UnaryServerInterceptor(verifier *Verifier) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "internal token is required")
		}
		values := md.Get("authorization")
		if len(values) != 1 || len(values[0]) < 8 || values[0][:7] != "Bearer " {
			return nil, status.Error(codes.Unauthenticated, "internal token is required")
		}
		claims, err := verifier.Verify(values[0][7:], info.FullMethod)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "internal token is invalid")
		}
		p := Principal{Caller: claims.Subject, ActorID: claims.ActorID, ActorRoles: append([]string(nil), claims.ActorRoles...), RequestID: claims.RequestID, TraceID: claims.TraceID, TokenID: claims.TokenID}
		return handler(WithPrincipal(ctx, p), req)
	}
}
