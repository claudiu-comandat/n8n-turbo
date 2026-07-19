package auth

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	cookieName                = "n8n-auth"
	contextUserKey contextKey = "auth-user"
)

var (
	ErrNoToken        = errors.New("no authentication token")
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token expired")
	ErrUserDisabled   = errors.New("user disabled")
	ErrBadCredentials = errors.New("invalid email or password")
)

type Claims struct {
	jwt.RegisteredClaims
	UserID    string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Role      string `json:"role"`
	IsOwner   bool   `json:"isOwner,omitempty"`
	IsMember  bool   `json:"isMember,omitempty"`
}

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
	IsOwner   bool   `json:"isOwner"`
}

func userFromClaims(claims *Claims) User {
	return User{
		ID:        claims.UserID,
		Email:     claims.Email,
		FirstName: claims.FirstName,
		LastName:  claims.LastName,
		Role:      claims.Role,
		IsOwner:   claims.IsOwner,
	}
}

func withUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, contextUserKey, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(contextUserKey).(User)
	return user, ok
}

// ContextWithUser attaches an authenticated user to the context. It lets non-JWT
// authenticators (e.g. an API-key guard) satisfy the same UserFromContext contract.
func ContextWithUser(ctx context.Context, user User) context.Context {
	return withUser(ctx, user)
}

func defaultExpiry(now time.Time, duration time.Duration) time.Time {
	return now.UTC().Add(duration)
}
