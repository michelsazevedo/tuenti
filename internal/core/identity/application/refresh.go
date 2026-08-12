package application

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

type RefreshUseCase interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
}

type refresh struct {
	refreshStore domain.RefreshTokenStore
	secret       string
}

func NewRefresh(refreshStore domain.RefreshTokenStore, conf *config.Config) RefreshUseCase {
	return &refresh{refreshStore: refreshStore, secret: conf.Settings.Secret}
}

func (s *refresh) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	rotatedToken, userID, err := s.refreshStore.Rotate(ctx, refreshToken, refreshTokenTTL)
	if err != nil {
		return nil, err
	}

	accessToken, err := s.newAccessToken(userID)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rotatedToken,
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
	}, nil
}

func (s *refresh) newAccessToken(subject string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.secret))
}
