// Package userctx carries a server-verified local identity across API handlers.
package userctx

import (
	"context"
	"time"
)

type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Admin    bool      `json:"admin"`
	Disabled bool      `json:"disabled"`
	Created  time.Time `json:"created"`
}
type Identity struct {
	User
	Home string
}
type key struct{}

func With(ctx context.Context, user Identity) context.Context {
	return context.WithValue(ctx, key{}, user)
}
func From(ctx context.Context) (Identity, bool) {
	user, ok := ctx.Value(key{}).(Identity)
	return user, ok && user.ID != "" && !user.Disabled
}
