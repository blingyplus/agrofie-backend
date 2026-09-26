package graph

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/graph/model"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
)

// Resolver helpers live here, not in schema.resolvers.go: gqlgen owns that file
// and comments out anything it does not recognise when regenerating.

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func toLookup(item *lookup.Item) *model.Lookup {
	return &model.Lookup{
		ID:        item.ID,
		Code:      item.Code,
		Name:      item.Name,
		SortOrder: int(item.SortOrder),
		IsActive:  item.IsActive,
	}
}

func listLookups(ctx context.Context, activeOnly bool, fn func(context.Context, bool) ([]lookup.Item, error)) ([]*model.Lookup, error) {
	rows, err := fn(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Lookup, 0, len(rows))
	for i := range rows {
		out = append(out, toLookup(&rows[i]))
	}
	return out, nil
}

func toAuthPayload(session *agrofierv1.AuthSession) *model.AuthPayload {
	if session == nil {
		return nil
	}
	return &model.AuthPayload{
		SessionToken: session.GetSessionToken(),
		User:         toModelUser(session.GetUser()),
	}
}

func toModelUser(u *agrofierv1.User) *model.User {
	if u == nil {
		return nil
	}
	var phone *string
	if u.GetPhone() != "" {
		p := u.GetPhone()
		phone = &p
	}
	return &model.User{
		ID:          u.GetId(),
		Email:       u.GetEmail(),
		Phone:       phone,
		DisplayName: u.GetDisplayName(),
		RoleCodes:   append([]string(nil), u.GetRoleCodes()...),
	}
}

func mapConnectErr(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return fmt.Errorf("%s", connectErr.Message())
	}
	return err
}
