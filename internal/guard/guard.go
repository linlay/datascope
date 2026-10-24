// Package guard centralizes platform authorization and trusted identity context.
package guard

import (
	"context"
	"fmt"
	"datascope-server/internal/management"
)

type key struct{}

func WithUser(ctx context.Context, u management.User) context.Context {
	return context.WithValue(ctx, key{}, u)
}
func User(ctx context.Context) (management.User, bool) {
	u, ok := ctx.Value(key{}).(management.User)
	return u, ok
}
func Require(ctx context.Context, s *management.Store, database, permission string) error {
	u, ok := User(ctx)
	if !ok || !s.Can(ctx, u.ID, database, permission) {
		return fmt.Errorf("%w: %s", management.ErrDenied, permission)
	}
	return nil
}
