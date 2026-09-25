package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
)

func Authenticated(ctx context.Context, obj any, next graphql.Resolver) (any, error) {
	if _, ok := gqlauth.FromContext(ctx); !ok {
		return nil, fmt.Errorf("unauthenticated")
	}
	return next(ctx)
}

func HasRole(ctx context.Context, obj any, next graphql.Resolver, code string) (any, error) {
	p, ok := gqlauth.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("unauthenticated")
	}
	if !p.HasRole(code) {
		return nil, fmt.Errorf("forbidden: requires role %s", code)
	}
	return next(ctx)
}
