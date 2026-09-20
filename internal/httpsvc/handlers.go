package httpsvc

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/gen/agrofie/v1/agrofierv1connect"
	"github.com/blingyplus/agrofie-backend/internal/health"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// AuthHandler serves /healthz and Connect AuthService.
func AuthHandler(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(serviceName))
	path, handler := agrofierv1connect.NewAuthServiceHandler(&authServer{name: serviceName})
	mux.Handle(path, handler)
	return h2c.NewHandler(mux, &http2.Server{})
}

type authServer struct{ name string }

func (s *authServer) Health(
	_ context.Context,
	_ *connect.Request[agrofierv1.HealthRequest],
) (*connect.Response[agrofierv1.HealthResponse], error) {
	return connect.NewResponse(&agrofierv1.HealthResponse{Status: "ok", Service: s.name}), nil
}

// BookingHandler serves /healthz and Connect BookingService.
func BookingHandler(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(serviceName))
	path, handler := agrofierv1connect.NewBookingServiceHandler(&bookingServer{name: serviceName})
	mux.Handle(path, handler)
	return h2c.NewHandler(mux, &http2.Server{})
}

type bookingServer struct{ name string }

func (s *bookingServer) Health(
	_ context.Context,
	_ *connect.Request[agrofierv1.HealthRequest],
) (*connect.Response[agrofierv1.HealthResponse], error) {
	return connect.NewResponse(&agrofierv1.HealthResponse{Status: "ok", Service: s.name}), nil
}

// PaymentsHandler serves /healthz and Connect PaymentsService.
func PaymentsHandler(serviceName string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(serviceName))
	path, handler := agrofierv1connect.NewPaymentsServiceHandler(&paymentsServer{name: serviceName})
	mux.Handle(path, handler)
	return h2c.NewHandler(mux, &http2.Server{})
}

type paymentsServer struct{ name string }

func (s *paymentsServer) Health(
	_ context.Context,
	_ *connect.Request[agrofierv1.HealthRequest],
) (*connect.Response[agrofierv1.HealthResponse], error) {
	return connect.NewResponse(&agrofierv1.HealthResponse{Status: "ok", Service: s.name}), nil
}
