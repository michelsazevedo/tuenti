package application

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

type LogoutUseCase interface {
	Logout(ctx context.Context, refreshToken string) error
}

type logout struct {
	refreshStore domain.RefreshTokenStore
}

func NewLogout(refreshStore domain.RefreshTokenStore) LogoutUseCase {
	return &logout{refreshStore: refreshStore}
}

func (s *logout) Logout(ctx context.Context, refreshToken string) error {
	return s.refreshStore.Revoke(ctx, refreshToken)
}
