package graph

import (
	"context"
	"net/http"
	"time"

	"github.com/blingyplus/agrofie-backend/gen/agrofie/v1/agrofierv1connect"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
)

// Resolver is the dependency root for GraphQL resolvers.
type Resolver struct {
	Lookups    *lookup.Service
	Prober     HealthProber
	AuthClient agrofierv1connect.AuthServiceClient
}

// HealthProber aggregates downstream service health.
type HealthProber interface {
	Probe(ctx context.Context) (auth, booking, payments string)
}

// NewAuthClient builds a Connect client for the auth service.
func NewAuthClient(authURL string) agrofierv1connect.AuthServiceClient {
	return agrofierv1connect.NewAuthServiceClient(&http.Client{Timeout: 15 * time.Second}, authURL)
}
