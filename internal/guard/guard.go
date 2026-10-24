// Package guard centralizes platform authorization and trusted identity context.
package guard

import (
	"context"
	"datascope-server/internal/management"
	"fmt"
	"slices"
)

type key struct{}
type businessKey struct{}

func WithBusiness(ctx context.Context, a management.BusinessAccount) context.Context {
	return context.WithValue(ctx, businessKey{}, a)
}
func Business(ctx context.Context) (management.BusinessAccount, bool) {
	a, ok := ctx.Value(businessKey{}).(management.BusinessAccount)
	return a, ok
}

func WithUser(ctx context.Context, u management.User) context.Context {
	return context.WithValue(ctx, key{}, u)
}
func User(ctx context.Context) (management.User, bool) {
	u, ok := ctx.Value(key{}).(management.User)
	return u, ok
}
func Require(ctx context.Context, s *management.Store, database, permission string) error {
	if a, ok := Business(ctx); ok {
		if a.Active && a.DatabaseID == database && slices.Contains(a.Permissions, permission) {
			return nil
		}
		return fmt.Errorf("%w: %s", management.ErrDenied, permission)
	}
	u, ok := User(ctx)
	if !ok || !s.Can(ctx, u.ID, database, permission) {
		return fmt.Errorf("%w: %s", management.ErrDenied, permission)
	}
	return nil
}
