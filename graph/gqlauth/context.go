package gqlauth

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/gen/agrofie/v1/agrofierv1connect"
)

type ctxKey int

const principalKey ctxKey = 1

// Principal is the authenticated marketplace user on a request.
type Principal struct {
	UserID      string
	Email       string
	Phone       string
	DisplayName string
	RoleCodes   []string
	Token       string
}

func (p *Principal) HasRole(code string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.RoleCodes {
		if r == code {
			return true
		}
	}
	return false
}

// FromContext returns the principal if present.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok && p != nil
}

// WithPrincipal stores the principal on the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// Middleware resolves Bearer tokens via auth WhoAmI and attaches a Principal.
func Middleware(client agrofierv1connect.AuthServiceClient) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r.Header.Get("Authorization"))
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			res, err := client.WhoAmI(r.Context(), connect.NewRequest(&agrofierv1.WhoAmIRequest{
				SessionToken: token,
			}))
			if err != nil || res.Msg.GetUser() == nil {
				next.ServeHTTP(w, r)
				return
			}
			u := res.Msg.GetUser()
			p := &Principal{
				UserID:      u.GetId(),
				Email:       u.GetEmail(),
				Phone:       u.GetPhone(),
				DisplayName: u.GetDisplayName(),
				RoleCodes:   append([]string(nil), u.GetRoleCodes()...),
				Token:       token,
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
