// Package connectrpc documents internal RPC contracts.
//
// ConnectRPC / gRPC schemas live in proto/agrofie/v1.
// Generate with: make proto
//
// Services currently expose HTTP /healthz for local Compose smoke tests.
// Wire generated Connect handlers from gen/ when domain RPCs are added.
// Spec: external GraphQL (gateway), internal Connect/gRPC between services.
package connectrpc

const (
	AuthServiceName     = "agrofie.v1.AuthService"
	BookingServiceName  = "agrofie.v1.BookingService"
	PaymentsServiceName = "agrofie.v1.PaymentsService"
)
