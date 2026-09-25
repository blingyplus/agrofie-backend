package httpsvc

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/gen/agrofie/v1/agrofierv1connect"
	"github.com/blingyplus/agrofie-backend/internal/auth"
	"github.com/blingyplus/agrofie-backend/internal/health"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// AuthHandler serves /healthz and Connect AuthService.
func AuthHandler(serviceName string, svc *auth.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(serviceName))
	path, handler := agrofierv1connect.NewAuthServiceHandler(&authServer{
		name: serviceName,
		svc:  svc,
	})
	mux.Handle(path, handler)
	return h2c.NewHandler(mux, &http2.Server{})
}

type authServer struct {
	name string
	svc  *auth.Service
}

func (s *authServer) Health(
	_ context.Context,
	_ *connect.Request[agrofierv1.HealthRequest],
) (*connect.Response[agrofierv1.HealthResponse], error) {
	return connect.NewResponse(&agrofierv1.HealthResponse{Status: "ok", Service: s.name}), nil
}

func (s *authServer) Register(
	ctx context.Context,
	req *connect.Request[agrofierv1.RegisterRequest],
) (*connect.Response[agrofierv1.RegisterResponse], error) {
	session, err := s.svc.Register(ctx, auth.RegisterInput{
		Email:       req.Msg.GetEmail(),
		Phone:       req.Msg.GetPhone(),
		Password:    req.Msg.GetPassword(),
		DisplayName: req.Msg.GetDisplayName(),
		RoleCode:    req.Msg.GetRoleCode(),
	})
	if err != nil {
		return nil, mapAuthError(err)
	}
	return connect.NewResponse(&agrofierv1.RegisterResponse{Session: toProtoSession(session)}), nil
}

func (s *authServer) Login(
	ctx context.Context,
	req *connect.Request[agrofierv1.LoginRequest],
) (*connect.Response[agrofierv1.LoginResponse], error) {
	session, err := s.svc.Login(ctx, auth.LoginInput{
		Identifier: req.Msg.GetIdentifier(),
		Password:   req.Msg.GetPassword(),
	})
	if err != nil {
		return nil, mapAuthError(err)
	}
	return connect.NewResponse(&agrofierv1.LoginResponse{Session: toProtoSession(session)}), nil
}

func (s *authServer) Logout(
	ctx context.Context,
	req *connect.Request[agrofierv1.LogoutRequest],
) (*connect.Response[agrofierv1.LogoutResponse], error) {
	if err := s.svc.Logout(ctx, req.Msg.GetSessionToken()); err != nil {
		return nil, mapAuthError(err)
	}
	return connect.NewResponse(&agrofierv1.LogoutResponse{}), nil
}

func (s *authServer) WhoAmI(
	ctx context.Context,
	req *connect.Request[agrofierv1.WhoAmIRequest],
) (*connect.Response[agrofierv1.WhoAmIResponse], error) {
	user, err := s.svc.WhoAmI(ctx, req.Msg.GetSessionToken())
	if err != nil {
		return nil, mapAuthError(err)
	}
	return connect.NewResponse(&agrofierv1.WhoAmIResponse{User: toProtoUser(user)}), nil
}

func toProtoSession(s *auth.Session) *agrofierv1.AuthSession {
	return &agrofierv1.AuthSession{
		SessionToken: s.Token,
		User:         toProtoUser(&s.User),
	}
}

func toProtoUser(u *auth.User) *agrofierv1.User {
	return &agrofierv1.User{
		Id:          u.ID,
		Email:       u.Email,
		Phone:       u.Phone,
		DisplayName: u.DisplayName,
		RoleCodes:   u.RoleCodes,
	}
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, auth.ErrForbiddenRole):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, auth.ErrConflict):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrUnauthorized):
		return connect.NewError(connect.CodeUnauthenticated, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
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
