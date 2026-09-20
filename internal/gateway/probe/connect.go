package probe

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/gen/agrofie/v1/agrofierv1connect"
)

// ConnectProber checks auth/booking/payments via Connect Health RPCs.
type ConnectProber struct {
	auth     agrofierv1connect.AuthServiceClient
	booking  agrofierv1connect.BookingServiceClient
	payments agrofierv1connect.PaymentsServiceClient
}

func NewConnectProber(authURL, bookingURL, paymentsURL string) *ConnectProber {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	return &ConnectProber{
		auth:     agrofierv1connect.NewAuthServiceClient(httpClient, authURL),
		booking:  agrofierv1connect.NewBookingServiceClient(httpClient, bookingURL),
		payments: agrofierv1connect.NewPaymentsServiceClient(httpClient, paymentsURL),
	}
}

func (p *ConnectProber) Probe(ctx context.Context) (auth, booking, payments string) {
	return p.call(ctx, p.auth.Health), p.call(ctx, p.booking.Health), p.call(ctx, p.payments.Health)
}

func (p *ConnectProber) call(ctx context.Context, fn func(context.Context, *connect.Request[agrofierv1.HealthRequest]) (*connect.Response[agrofierv1.HealthResponse], error)) string {
	res, err := fn(ctx, connect.NewRequest(&agrofierv1.HealthRequest{}))
	if err != nil {
		return "unreachable"
	}
	if res.Msg.GetStatus() == "ok" {
		return "ok"
	}
	return fmt.Sprintf("status_%s", res.Msg.GetStatus())
}
