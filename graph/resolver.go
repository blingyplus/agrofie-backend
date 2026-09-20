package graph

import (
	"context"

	"github.com/blingyplus/agrofie-backend/internal/lookup"
)

// Resolver is the dependency root for GraphQL resolvers.
type Resolver struct {
	Lookups *lookup.Service
	Prober  HealthProber
}

// HealthProber aggregates downstream service health.
type HealthProber interface {
	Probe(ctx context.Context) (auth, booking, payments string)
}
