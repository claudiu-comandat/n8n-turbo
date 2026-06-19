package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/n8n-io/n8n-turbo/internal/config"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type Service struct {
	users     persistence.UserStore
	jwtSecret []byte
	config    config.AuthConfig
}

type LoginResult struct {
	User      User      `json:"user"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func NewService(users persistence.UserStore, encryptionKey string, cfg config.AuthConfig) *Service {
	return &Service{
		users:     users,
		jwtSecret: []byte(encryptionKey),
		config:    cfg,
	}
}

func (s *Service) BootstrapOwner(ctx context.Context) error {
	hasAny, err := s.users.HasAny(ctx)
	if err != nil {
		return fmt.Errorf("check users: %w", err)
	}
	if hasAny {
		return nil
	}

	hash, err := HashPassword(s.config.SetupPassword)
	if err != nil {
		return fmt.Errorf("hash setup password: %w", err)
	}

	return s.users.Insert(ctx, persistence.UserRow{
		Email:     s.config.SetupEmail,
		FirstName: s.config.SetupFirstName,
		LastName:  s.config.SetupLastName,
		Password:  &hash,
		Role:      "global:owner",
	})
}

func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	user, err := s.users.GetByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if err == persistence.ErrNotFound {
			_, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
			return nil, ErrBadCredentials
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user.Disabled {
		return nil, ErrUserDisabled
	}
	if user.Password == nil || *user.Password == "" {
		return nil, ErrBadCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.Password), []byte(password)); err != nil {
		return nil, ErrBadCredentials
	}

	token, expiresAt, err := s.generateToken(*user)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &LoginResult{
		User: User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Role:      user.Role,
			IsOwner:   user.Role == "global:owner",
		},
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "expired") {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (s *Service) GetUser(ctx context.Context, userID string) (*persistence.UserRow, error) {
	return s.users.GetByID(ctx, userID)
}

func (s *Service) generateToken(user persistence.UserRow) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := defaultExpiry(now, s.config.JWTDuration)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   user.ID,
		},
		UserID:    user.ID,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Role:      user.Role,
		IsOwner:   user.Role == "global:owner",
		IsMember:  user.Role != "global:owner",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}
